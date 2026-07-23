package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var modelsRoot string

func main() {
	startTime = time.Now()

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	modelsRoot = os.Getenv("MODELS_ROOT")
	if modelsRoot == "" {
		modelsRoot = "/data/models"
	}

	buildDir := filepath.Join(".", "build")
	if _, err := os.Stat(buildDir); os.IsNotExist(err) {
		buildDir = "/app/build"
	}

	// Initialize auth system (load saved users)
	initAuth()

	mux := http.NewServeMux()

	// Auth routes
	mux.HandleFunc("/api/auth/login", handleLogin)
	mux.HandleFunc("/api/auth/logout", handleLogout)
	mux.HandleFunc("/api/auth/me", handleMe)
	mux.HandleFunc("/api/auth/register", handleRegister)

	// Model routes
	mux.HandleFunc("/api/models", handleModels)
	mux.HandleFunc("/api/thumbnail", handleThumbnail)

	// Scan directory
	mux.HandleFunc("/api/scan-directory", handleScanDirectory)

	// Export routes
	mux.HandleFunc("/api/export-file", handleExportFile)
	mux.HandleFunc("/api/append-to-list", handleAppendToList)

	// File routes
	mux.HandleFunc("/api/files", handleFiles)
	mux.HandleFunc("/api/file", handleFile)
	mux.HandleFunc("/api/fetch-url", handleFetchURL)

	// Settings & diagnostic routes
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleGetSettings(w, r)
		case http.MethodPut:
			handleUpdateSettings(w, r)
		default:
			jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/diagnose", handleDiagnose)
	mux.HandleFunc("/api/rescan", handleRescan)

	// Static files: serve /lib/* and /_app/* etc. from build/
	fileServer := http.FileServer(http.Dir(buildDir))
	mux.Handle("/lib/", fileServer)
	mux.Handle("/_app/", fileServer)
	mux.Handle("/favicon.png", fileServer)
	mux.Handle("/cursors/", fileServer)

	// SPA fallback: all other GET requests return index.html
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		indexPath := filepath.Join(buildDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			http.ServeFile(w, r, indexPath)
		} else {
			http.NotFound(w, r)
		}
	})

	fmt.Printf("Spive2D server running on http://localhost:%s\n", port)
	fmt.Printf("Models root: %s\n", modelsRoot)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// --- API: GET /api/files?path=... ---
func handleFiles(w http.ResponseWriter, r *http.Request) {
	inputPath := r.URL.Query().Get("path")
	if inputPath == "" {
		jsonError(w, "Missing path parameter", http.StatusBadRequest)
		return
	}
	safePath, err := resolvePath(inputPath)
	if err != nil {
		jsonError(w, err.Error(), http.StatusForbidden)
		return
	}
	info, err := os.Stat(safePath)
	if err != nil {
		jsonError(w, "Path not found", http.StatusNotFound)
		return
	}
	if !info.IsDir() {
		jsonError(w, "Path is not a directory", http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(safePath)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type FileEntry struct {
		Name  string `json:"name"`
		Size  int64  `json:"size"`
		IsDir bool   `json:"isDir"`
	}
	var files []FileEntry
	for _, e := range entries {
		fi, _ := e.Info()
		files = append(files, FileEntry{
			Name:  e.Name(),
			Size:  fi.Size(),
			IsDir: e.IsDir(),
		})
	}
	jsonResp(w, map[string]interface{}{"files": files})
}

// --- API: GET /api/file?path=... ---
func handleFile(w http.ResponseWriter, r *http.Request) {
	inputPath := r.URL.Query().Get("path")
	if inputPath == "" {
		jsonError(w, "Missing path parameter", http.StatusBadRequest)
		return
	}
	safePath, err := resolvePath(inputPath)
	if err != nil {
		jsonError(w, err.Error(), http.StatusForbidden)
		return
	}
	if _, err := os.Stat(safePath); os.IsNotExist(err) {
		jsonError(w, "File not found", http.StatusNotFound)
		return
	}
	// Set content type based on extension
	ext := strings.ToLower(filepath.Ext(safePath))
	mimeTypes := map[string]string{
		".js":   "application/javascript",
		".json": "application/json",
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".webp": "image/webp",
		".moc3": "application/octet-stream",
		".skel": "application/octet-stream",
		".atlas": "text/plain",
	}
	if mime, ok := mimeTypes[ext]; ok {
		w.Header().Set("Content-Type", mime)
	}
	http.ServeFile(w, r, safePath)
}

// --- API: POST /api/fetch-url ---
func handleFetchURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		jsonError(w, "Missing url parameter", http.StatusBadRequest)
		return
	}
	resp, err := http.Get(body.URL)
	if err != nil {
		jsonError(w, "Fetch failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		jsonError(w, "Read failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(data)
}

// --- Helpers ---
func resolvePath(inputPath string) (string, error) {
	abs, err := filepath.Abs(inputPath)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	root, _ := filepath.Abs(modelsRoot)
	if strings.HasPrefix(abs, root+string(filepath.Separator)) || abs == root {
		return abs, nil
	}
	// Also allow access to temp dir and home dir
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(abs, home+string(filepath.Separator)) {
		return abs, nil
	}
	return "", fmt.Errorf("access denied: path not allowed")
}

func resolveExportPath(inputPath string) (string, error) {
	abs, err := filepath.Abs(inputPath)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	root, _ := filepath.Abs(modelsRoot)
	if strings.HasPrefix(abs, root+string(filepath.Separator)) || abs == root {
		return abs, nil
	}
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(abs, home+string(filepath.Separator)) {
		return abs, nil
	}
	return "", fmt.Errorf("access denied: export path not allowed")
}

func decodeBase64(s string) []byte {
	// Try standard base64 first
	data, err := base64.StdEncoding.DecodeString(s)
	if err == nil {
		return data
	}
	// Try URL-safe base64
	data, err = base64.URLEncoding.DecodeString(s)
	if err == nil {
		return data
	}
	return nil
}

func jsonResp(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
