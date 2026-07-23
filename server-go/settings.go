package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config represents the application configuration
type Config struct {
	ModelsRoot    string `json:"modelsRoot"`
	LibraryPath   string `json:"libraryPath"`
	Version       string `json:"version"`
	ScanOnStartup bool   `json:"scanOnStartup"`
	ThumbnailSize int    `json:"thumbnailSize"`
}

// SystemDiagnosis represents the system diagnostic information
type SystemDiagnosis struct {
	Status      string     `json:"status"`
	ModelsRoot  DirCheck   `json:"modelsRoot"`
	BuildDir    DirCheck   `json:"buildDir"`
	DataDir     DirCheck   `json:"dataDir"`
	ModelsCount int        `json:"modelsCount"`
	LibsCheck   []LibCheck `json:"libsCheck"`
	Uptime      string     `json:"uptime"`
}

// DirCheck represents a directory accessibility check result
type DirCheck struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Readable bool   `json:"readable"`
	IsDir    bool   `json:"isDir"`
	Writable bool   `json:"writable"`
}

// LibCheck represents a runtime library file check result
type LibCheck struct {
	Name   string `json:"name"`
	Exists bool   `json:"exists"`
	Size   int64  `json:"size"`
}

var startTime time.Time

func getConfigPath() string {
	dir := getEnv("DATA_DIR", "/data")
	return filepath.Join(dir, "config.json")
}

func defaultConfig() Config {
	return Config{
		ModelsRoot:    getEnv("MODELS_ROOT", "/data/models"),
		LibraryPath:   getEnv("MODELS_ROOT", "/data/models"),
		Version:       "1.0.0",
		ScanOnStartup: true,
		ThumbnailSize: 200,
	}
}

func loadConfig() Config {
	cfg := defaultConfig()
	data, err := os.ReadFile(getConfigPath())
	if err != nil {
		return cfg
	}
	var saved Config
	if err := json.Unmarshal(data, &saved); err != nil {
		return cfg
	}
	// Merge: use saved values where non-zero, fall back to defaults
	if saved.ModelsRoot != "" {
		cfg.ModelsRoot = saved.ModelsRoot
	}
	if saved.LibraryPath != "" {
		cfg.LibraryPath = saved.LibraryPath
	}
	if saved.Version != "" {
		cfg.Version = saved.Version
	}
	cfg.ScanOnStartup = saved.ScanOnStartup
	if saved.ThumbnailSize > 0 {
		cfg.ThumbnailSize = saved.ThumbnailSize
	}
	return cfg
}

func saveConfig(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(getConfigPath())
	os.MkdirAll(dir, 0755)
	return os.WriteFile(getConfigPath(), data, 0644)
}

func checkDir(path string) DirCheck {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return DirCheck{
			Path:   path,
			Exists: false,
		}
	}
	if err != nil {
		return DirCheck{
			Path:   path,
			Exists: true,
		}
	}

	// Check readable: try to open the dir
	readable := true
	f, err := os.Open(path)
	if err != nil {
		readable = false
	} else {
		f.Close()
	}

	// Check writable: try to create a temp file
	writable := true
	tmpFile := filepath.Join(path, ".write_test")
	if err := os.WriteFile(tmpFile, []byte{}, 0644); err != nil {
		writable = false
	} else {
		os.Remove(tmpFile)
	}

	return DirCheck{
		Path:     path,
		Exists:   true,
		Readable: readable,
		IsDir:    info.IsDir(),
		Writable: writable,
	}
}

func collectLibChecks() []LibCheck {
	buildDir := filepath.Join(".", "build", "lib")
	if _, err := os.Stat(buildDir); os.IsNotExist(err) {
		buildDir = "/app/build/lib"
	}

	libNames := []string{
		"live2dcubismcore.js",
		"live2dcubismcore.min.js",
		"spine-core.js",
		"spine-canvas.js",
	}

	var checks []LibCheck
	for _, name := range libNames {
		libPath := filepath.Join(buildDir, name)
		info, err := os.Stat(libPath)
		exists := err == nil
		var size int64
		if exists {
			size = info.Size()
		}
		checks = append(checks, LibCheck{
			Name:   name,
			Exists: exists,
			Size:   size,
		})
	}
	return checks
}

// handleGetSettings handles GET /api/settings
func handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := loadConfig()
	jsonResp(w, cfg)
}

// handleUpdateSettings handles PUT /api/settings
func handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var cfg Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := saveConfig(cfg); err != nil {
		jsonError(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResp(w, map[string]interface{}{
		"status": "ok",
		"config": cfg,
	})
}

// handleDiagnose handles GET /api/diagnose
func handleDiagnose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	modelsRoot := getEnv("MODELS_ROOT", "/data/models")
	buildDir := filepath.Join(".", "build")
	if _, err := os.Stat(buildDir); os.IsNotExist(err) {
		buildDir = "/app/build"
	}
	dataDir := getEnv("DATA_DIR", "/data")

	modelsRootCheck := checkDir(modelsRoot)
	buildDirCheck := checkDir(buildDir)
	dataDirCheck := checkDir(dataDir)

	// Count models
	modelsCount := 0
	if modelsRootCheck.Exists {
		entries, err := os.ReadDir(modelsRoot)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
					modelsCount++
				}
			}
		}
	}

	libsCheck := collectLibChecks()

	// Determine overall status
	status := "ok"
	if !modelsRootCheck.Exists || !modelsRootCheck.Readable {
		status = "error"
	} else if modelsCount == 0 {
		status = "warning"
	}

	uptime := time.Since(startTime).Round(time.Second).String()

	diag := SystemDiagnosis{
		Status:      status,
		ModelsRoot:  modelsRootCheck,
		BuildDir:    buildDirCheck,
		DataDir:     dataDirCheck,
		ModelsCount: modelsCount,
		LibsCheck:   libsCheck,
		Uptime:      uptime,
	}

	jsonResp(w, diag)
}

// handleRescan handles POST /api/rescan - triggers model scan logic
func handleRescan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Re-trigger the handleModels logic
	handleModels(w, r)
}
