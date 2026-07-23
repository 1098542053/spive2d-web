package main

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ModelInfo struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"` // "live2d", "spine"
	HasThumb   bool   `json:"hasThumb"`
	ThumbPath  string `json:"thumbPath,omitempty"`
	SceneCount int    `json:"sceneCount"`
}

func handleModels(w http.ResponseWriter, r *http.Request) {
	user := getSessionUser(r)
	if user == nil {
		jsonError(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	modelsRoot := getEnv("MODELS_ROOT", "/data/models")
	info, err := os.Stat(modelsRoot)
	if err != nil || !info.IsDir() {
		jsonResp(w, map[string]interface{}{"models": []ModelInfo{}})
		return
	}

	entries, _ := os.ReadDir(modelsRoot)
	var models []ModelInfo
	thumbDir := filepath.Join(modelsRoot, ".thumbs")
	os.MkdirAll(thumbDir, 0755)

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dirPath := filepath.Join(modelsRoot, entry.Name())
		modelType, sceneCount := detectModelType(dirPath)
		if modelType == "" {
			continue
		}
		thumbPath := filepath.Join(thumbDir, entry.Name()+".png")
		hasThumb := false
		if _, err := os.Stat(thumbPath); err == nil {
			hasThumb = true
		}
		models = append(models, ModelInfo{
			Name:       entry.Name(),
			Path:       dirPath,
			Type:       modelType,
			HasThumb:   hasThumb,
			ThumbPath:  "/api/thumbnail?name=" + entry.Name(),
			SceneCount: sceneCount,
		})
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].Name < models[j].Name
	})

	jsonResp(w, map[string]interface{}{"models": models})
}

func handleThumbnail(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.NotFound(w, r)
		return
	}
	modelsRoot := getEnv("MODELS_ROOT", "/data/models")
	thumbPath := filepath.Join(modelsRoot, ".thumbs", name+".png")
	if _, err := os.Stat(thumbPath); err == nil {
		w.Header().Set("Content-Type", "image/png")
		http.ServeFile(w, r, thumbPath)
		return
	}
	// Return a placeholder SVG thumbnail
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="280" viewBox="0 0 200 280">
  <rect width="200" height="280" fill="#1a1a2e" rx="8"/>
  <text x="100" y="130" text-anchor="middle" fill="#555" font-size="14" font-family="sans-serif">` +
		name + `</text>
  <text x="100" y="160" text-anchor="middle" fill="#444" font-size="12" font-family="sans-serif">Click to view</text>
</svg>`
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Write([]byte(svg))
}

func detectModelType(dirPath string) (string, int) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return "", 0
	}

	hasMoc3 := false
	hasAtlas := false
	hasSkel := false
	hasSpineJSON := false
	count := 0

	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if strings.HasSuffix(name, ".moc3") {
			hasMoc3 = true
			count++
		}
		if strings.Contains(name, ".atlas") && !strings.HasSuffix(name, ".png") {
			hasAtlas = true
		}
		if strings.HasSuffix(name, ".skel") {
			hasSkel = true
			count++
		}
		if strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".model3.json") {
			// Check if it has .atlas companion
			base := strings.TrimSuffix(e.Name(), ".json")
			atlasPath := filepath.Join(dirPath, base+".atlas")
			if _, err := os.Stat(atlasPath); err == nil {
				hasSpineJSON = true
				count++
			}
		}
	}

	// Also check subdirectories for models
	subEntries, _ := os.ReadDir(dirPath)
	for _, e := range subEntries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			subType, subCount := detectModelType(filepath.Join(dirPath, e.Name()))
			if subType != "" {
				count += subCount
				if subType == "live2d" {
					hasMoc3 = true
				} else {
					hasAtlas = true
				}
			}
		}
	}

	if hasMoc3 {
		return "live2d", count
	}
	if hasAtlas && (hasSkel || hasSpineJSON) {
		return "spine", count
	}

	return "", 0
}
