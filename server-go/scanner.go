package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SceneData represents a single scene/group within a model directory
type SceneData struct {
	Name     string   `json:"name"`
	MainExt  string   `json:"mainExt"`
	AtlasExt string   `json:"atlasExt"`
	Files    []string `json:"files"`
	IsMerged bool     `json:"isMerged"`
}

// scanDirectoryRequest is the POST body for /api/scan-directory
type scanDirectoryRequest struct {
	Path           string `json:"path"`
	MergeSequential bool   `json:"mergeSequential"`
	SkipUnity      bool   `json:"skipUnity"`
}

func handleScanDirectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := getSessionUser(r)
	if user == nil {
		jsonError(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	var req scanDirectoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Path == "" {
		jsonError(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	safePath, err := resolvePath(req.Path)
	if err != nil {
		jsonError(w, "Access denied: path not allowed", http.StatusForbidden)
		return
	}

	info, err := os.Stat(safePath)
	if os.IsNotExist(err) {
		jsonError(w, "Path not found", http.StatusNotFound)
		return
	}
	if !info.IsDir() {
		jsonError(w, "Path is not a directory", http.StatusBadRequest)
		return
	}

	dirFilesMap := processDirectoryWithSubdirs(safePath, safePath)

	if len(dirFilesMap) == 0 {
		jsonError(w, "No supported Spine (.atlas) or Live2D (.moc3) models found in directory", http.StatusNotFound)
		return
	}

	jsonResp(w, dirFilesMap)
}

// processDirectoryWithSubdirs recursively scans a directory and returns a map
// of directory paths to their scene data groups.
func processDirectoryWithSubdirs(dirPath string, basePath string) map[string][]SceneData {
	dirFilesMap := make(map[string][]SceneData)

	currentFileGroups := processFiles(dirPath, basePath)
	if len(currentFileGroups) > 0 {
		normalizedPath := strings.ReplaceAll(dirPath, "\\", "/")
		if !strings.HasSuffix(normalizedPath, "/") {
			normalizedPath += "/"
		}
		dirFilesMap[normalizedPath] = currentFileGroups
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return dirFilesMap
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		entryPath := filepath.Join(dirPath, entry.Name())
		if skipDir(entryPath) {
			continue
		}

		subdirFileGroups := processDirectoryWithSubdirs(entryPath, basePath)
		for k, v := range subdirFileGroups {
			if len(v) > 0 {
				normalizedSubdirPath := strings.ReplaceAll(k, "\\", "/")
				if !strings.HasSuffix(normalizedSubdirPath, "/") {
					normalizedSubdirPath += "/"
				}
				dirFilesMap[normalizedSubdirPath] = v
			}
		}
	}

	return dirFilesMap
}

// skipDir returns true if the directory should be skipped (hidden or temp)
func skipDir(dirPath string) bool {
	name := filepath.Base(dirPath)
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == ".thumbs"
}

// processFiles scans a single directory for Live2D and Spine files.
func processFiles(dirPath string, basePath string) []SceneData {
	var fileGroups []SceneData

	// File collection maps
	allAtlasInfo := make(map[string]string) // baseName -> extension
	var moc3Files []string
	var mocFiles []string
	var dirFiles []string
	hasMetaJson := false
	var metaJsonFiles []string

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fileGroups
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filename := entry.Name()
		dirFiles = append(dirFiles, filename)
		filenameLower := strings.ToLower(filename)

		if strings.Contains(filenameLower, ".atlas") && !strings.HasSuffix(filenameLower, ".png") {
			if !strings.HasSuffix(filenameLower, ".jpg") && !strings.HasSuffix(filenameLower, ".jpeg") && !strings.HasSuffix(filenameLower, ".webp") {
				idx := strings.LastIndex(filenameLower, ".atlas")
				if idx != -1 {
					baseNamePart := filename[:idx]
					extensionPart := filename[idx:]
					allAtlasInfo[baseNamePart] = extensionPart
				}
			}
		} else if strings.HasSuffix(filenameLower, ".moc3") {
			moc3Files = append(moc3Files, filename)
		} else if strings.HasSuffix(filenameLower, ".moc") {
			// Check it's not .moc3
			if !strings.HasSuffix(filenameLower, ".moc3") {
				mocFiles = append(mocFiles, filename)
			}
		} else if filenameLower == "meta.json" {
			hasMetaJson = true
		} else if strings.HasSuffix(filenameLower, ".meta.json") {
			metaJsonFiles = append(metaJsonFiles, filename)
		}
	}

	// Create relative path helper
	relPath := func(filename string) string {
		fullPath := filepath.Join(dirPath, filename)
		rp, _ := filepath.Rel(basePath, fullPath)
		return strings.ReplaceAll(rp, "\\", "/")
	}

	// Process .moc3 files (Live2D)
	for _, filename := range moc3Files {
		filenameLower := strings.ToLower(filename)
		adjustedPath := relPath(filename)
		moc3Pos := strings.Index(filenameLower, ".moc3")
		if moc3Pos != -1 {
			mocStem := filename[:moc3Pos]
			model3JsonPath := filepath.Join(dirPath, mocStem+".model3.json")
			if _, err := os.Stat(model3JsonPath); os.IsNotExist(err) {
				autoGenerateModel3Json(dirPath, filename, mocStem, dirFiles)
			}

			// Calculate baseNamePart (stem as it appears in relative path)
			baseNamePart := adjustedPath
			if len(adjustedPath) > len(filename) {
				baseNamePart = adjustedPath[:len(adjustedPath)-len(filename)+moc3Pos]
			} else {
				baseNamePart = mocStem
			}

			fileGroups = append(fileGroups, SceneData{
				Name:     baseNamePart,
				MainExt:  filename[moc3Pos:],
				AtlasExt: "",
				Files:    []string{},
				IsMerged: false,
			})
		}
	}

	// Process .moc files (older Live2D)
	for _, filename := range mocFiles {
		filenameLower := strings.ToLower(filename)
		adjustedPath := relPath(filename)
		mocPos := strings.Index(filenameLower, ".moc")
		if mocPos != -1 {
			baseNamePart := adjustedPath
			if len(adjustedPath) > len(filename) {
				baseNamePart = adjustedPath[:len(adjustedPath)-len(filename)+mocPos]
			} else {
				baseNamePart = filename[:mocPos]
			}
			fileGroups = append(fileGroups, SceneData{
				Name:     baseNamePart,
				MainExt:  filename[mocPos:],
				AtlasExt: "",
				Files:    []string{},
				IsMerged: false,
			})
		}
	}

	// Process meta.json
	if hasMetaJson {
		fileGroups = append(fileGroups, SceneData{
			Name:     "meta",
			MainExt:  ".json",
			AtlasExt: "",
			Files:    []string{},
			IsMerged: false,
		})
	}

	// Process *.meta.json files
	for _, metaFilename := range metaJsonFiles {
		stem := metaFilename
		if strings.HasSuffix(metaFilename, ".meta.json") {
			stem = metaFilename[:len(metaFilename)-10]
		}
		fileGroups = append(fileGroups, SceneData{
			Name:     stem,
			MainExt:  ".meta.json",
			AtlasExt: "",
			Files:    []string{},
			IsMerged: false,
		})
	}

	// Process atlas files (Spine)
	atlasBases := make(map[string]string) // baseName -> extension
	potentialExtraAtlases := make(map[string]string)

	for baseName, ext := range allAtlasInfo {
		baseLower := strings.ToLower(baseName)
		if strings.HasSuffix(baseLower, "_bg") || strings.HasSuffix(baseLower, "_fg") {
			potentialExtraAtlases[baseName] = ext
		} else {
			atlasBases[baseName] = ext
		}
	}

	// Handle extra atlases (_bg, _fg)
	for extraBaseName, ext := range potentialExtraAtlases {
		extraBaseLower := strings.ToLower(extraBaseName)
		hasCorresponding := false
		for baseName := range atlasBases {
			if strings.ToLower(baseName) == strings.TrimSuffix(extraBaseLower, "_bg") ||
				strings.ToLower(baseName) == strings.TrimSuffix(extraBaseLower, "_fg") {
				hasCorresponding = true
				break
			}
		}
		if !hasCorresponding {
			atlasBases[extraBaseName] = ext
		}
	}

	// Build file path map: lower filename -> relative path
	filePathMap := make(map[string]string)
	for _, entry := range entries {
		if !entry.IsDir() {
			filePathMap[strings.ToLower(entry.Name())] = relPath(entry.Name())
		}
	}

	for baseName := range atlasBases {
		baseLower := strings.ToLower(baseName)
		atlasExtension := allAtlasInfo[baseName]

		// Try .skel
		mainFileInfo := findMainFile(filePathMap, baseLower, ".skel")

		// Try .json
		if mainFileInfo == nil {
			mainFileInfo = findMainFile(filePathMap, baseLower, ".json")
		}

		// Try .asset
		if mainFileInfo == nil {
			mainFileInfo = findMainFile(filePathMap, baseLower, ".asset")
		}

		if mainFileInfo != nil {
			adjustedBaseName := mainFileInfo.path
			if idx := strings.Index(adjustedBaseName, "/"); idx != -1 {
				adjustedBaseName = adjustedBaseName[idx+1:]
			}
			adjustedBaseName = adjustedBaseName[:len(adjustedBaseName)-len(mainFileInfo.ext)]

			extraFiles := findExtraFiles(baseLower, filePathMap, mainFileInfo.ext)

			fileGroups = append(fileGroups, SceneData{
				Name:     adjustedBaseName,
				MainExt:  mainFileInfo.ext,
				AtlasExt: atlasExtension,
				Files:    extraFiles,
				IsMerged: false,
			})
		}
	}

	// Sort naturally by name
	sort.Slice(fileGroups, func(i, j int) bool {
		return compareNatural(fileGroups[i].Name, fileGroups[j].Name)
	})

	return fileGroups
}

type mainFileInfo struct {
	path string
	ext  string
	typ  string
}

func findMainFile(filePathMap map[string]string, baseLower string, ext string) *mainFileInfo {
	targetPattern := baseLower + ext
	for filenameLower, rp := range filePathMap {
		pos := strings.Index(filenameLower, targetPattern)
		if pos != -1 {
			originalFn := rp
			if idx := strings.LastIndex(rp, "/"); idx != -1 {
				originalFn = rp[idx+1:]
			}
			extStart := pos + len(baseLower)
			if extStart <= len(originalFn) {
				extPart := originalFn[extStart:]
				return &mainFileInfo{path: rp, ext: extPart, typ: ext[1:]}
			}
		}
	}
	return nil
}

func findExtraFiles(baseLower string, filePathMap map[string]string, mainExt string) []string {
	var extraFiles []string
	for _, suffix := range []string{"_bg", "_fg"} {
		extraBase := baseLower + suffix
		targetPattern := extraBase + mainExt
		for filenameLower, _ := range filePathMap {
			if strings.Contains(filenameLower, targetPattern) {
				// Find the original filename from the map
				for fnLower, rp := range filePathMap {
					if fnLower == filenameLower {
						stem := rp
						if idx := strings.LastIndex(rp, "/"); idx != -1 {
							stem = rp[idx+1:]
						}
						extraFiles = append(extraFiles, stem)
						break
					}
				}
			}
		}
	}
	return extraFiles
}

// autoGenerateModel3Json generates a .model3.json file for Live2D models that lack one.
func autoGenerateModel3Json(dirPath string, moc3Filename string, mocStem string, dirFiles []string) {
	type Motion struct {
		File string `json:"File"`
	}
	type Expression struct {
		File string `json:"File"`
	}
	type Texture struct {
		File string `json:"File"`
	}
	type Model3JSON struct {
		Version     int           `json:"Version"`
		FileReferences struct {
			Moc       string       `json:"Moc"`
			Textures  []Texture    `json:"Textures"`
			Motions   map[string][]Motion    `json:"Motions,omitempty"`
			Expressions []Expression `json:"Expressions,omitempty"`
		} `json:"FileReferences"`
		Groups []map[string]interface{} `json:"Groups,omitempty"`
	}

	model3 := Model3JSON{
		Version: 3,
		FileReferences: struct {
			Moc         string                `json:"Moc"`
			Textures    []Texture             `json:"Textures"`
			Motions     map[string][]Motion   `json:"Motions,omitempty"`
			Expressions []Expression          `json:"Expressions,omitempty"`
		}{
			Moc:      moc3Filename,
			Textures: []Texture{},
		},
	}

	// Check for textures directory
	texDir := filepath.Join(dirPath, "textures")
	if info, err := os.Stat(texDir); err == nil && info.IsDir() {
		texEntries, _ := os.ReadDir(texDir)
		for _, e := range texEntries {
			if !e.IsDir() {
				name := strings.ToLower(e.Name())
				if strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".jpg") ||
					strings.HasSuffix(name, ".jpeg") || strings.HasSuffix(name, ".webp") {
					model3.FileReferences.Textures = append(model3.FileReferences.Textures, Texture{
						File: "textures/" + e.Name(),
					})
				}
			}
		}
	}

	// If no textures directory, look for texture files in the same directory
	if len(model3.FileReferences.Textures) == 0 {
		for _, f := range dirFiles {
			lower := strings.ToLower(f)
			if strings.HasPrefix(lower, "texture") && (strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".jpg")) {
				model3.FileReferences.Textures = append(model3.FileReferences.Textures, Texture{File: f})
			}
		}
	}

	// Find motion files
	var motions []Motion
	var expressions []Expression
	for _, f := range dirFiles {
		lower := strings.ToLower(f)
		if strings.HasSuffix(lower, ".motion3.json") {
			motions = append(motions, Motion{File: f})
		} else if strings.HasSuffix(lower, ".exp3.json") {
			expressions = append(expressions, Expression{File: f})
		}
	}

	if len(motions) > 0 {
		model3.FileReferences.Motions = map[string][]Motion{
			"Idle":    motions,
			"TapBody": motions,
		}
	}
	if len(expressions) > 0 {
		model3.FileReferences.Expressions = expressions
	}

	// Write model3.json
	data, _ := json.MarshalIndent(model3, "", "  ")
	model3Path := filepath.Join(dirPath, mocStem+".model3.json")
	os.WriteFile(model3Path, data, 0644)
}

// compareNatural does a natural (human-friendly) string comparison
func compareNatural(a, b string) bool {
	a = strings.ToLower(a)
	b = strings.ToLower(b)

	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if isDigit(a[i]) && isDigit(b[j]) {
			numA := 0
			for i < len(a) && isDigit(a[i]) {
				numA = numA*10 + int(a[i]-'0')
				i++
			}
			numB := 0
			for j < len(b) && isDigit(b[j]) {
				numB = numB*10 + int(b[j]-'0')
				j++
			}
			if numA != numB {
				return numA < numB
			}
		} else {
			if a[i] != b[j] {
				return a[i] < b[j]
			}
			i++
			j++
		}
	}
	return len(a) < len(b)
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// --- API: POST /api/export-file ---
func handleExportFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := getSessionUser(r)
	if user == nil {
		jsonError(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	var req struct {
		Dir      string `json:"dir"`
		Filename string `json:"filename"`
		Data     string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.Dir == "" || req.Filename == "" || req.Data == "" {
		jsonError(w, "Missing dir, filename, or data parameter", http.StatusBadRequest)
		return
	}

	safeDir, err := resolveExportPath(req.Dir)
	if err != nil {
		jsonError(w, err.Error(), http.StatusForbidden)
		return
	}

	os.MkdirAll(safeDir, 0755)
	filePath := filepath.Join(safeDir, req.Filename)

	// Decode base64 data
	data := decodeBase64(req.Data)
	if data == nil {
		jsonError(w, "Invalid base64 data", http.StatusBadRequest)
		return
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		jsonError(w, "Failed to write file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, map[string]interface{}{
		"success": true,
		"path":    strings.ReplaceAll(filePath, "\\", "/"),
	})
}

// --- API: POST /api/append-to-list ---
func handleAppendToList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := getSessionUser(r)
	if user == nil {
		jsonError(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	var req struct {
		Dir      string `json:"dir"`
		Filename string `json:"filename"`
		Line     string `json:"line"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.Dir == "" || req.Filename == "" || req.Line == "" {
		jsonError(w, "Missing dir, filename, or line parameter", http.StatusBadRequest)
		return
	}

	safeDir, err := resolveExportPath(req.Dir)
	if err != nil {
		jsonError(w, err.Error(), http.StatusForbidden)
		return
	}

	os.MkdirAll(safeDir, 0755)
	filePath := filepath.Join(safeDir, req.Filename)

	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		jsonError(w, "Failed to open file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	if _, err := f.WriteString(req.Line + "\n"); err != nil {
		jsonError(w, "Failed to write: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, map[string]string{"status": "ok"})
}
