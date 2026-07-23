package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// These vars are set via -ldflags at build time.
// Defaults are used when running in dev mode (without -ldflags).
var (
	buildVersion = "dev"
	buildTime    = "unknown"
	gitRepoOwner = "1098542053"
	gitRepoName  = "spive2d-web"
)

// VersionInfo represents the version response
type VersionInfo struct {
	Version        string `json:"version"`
	BuildTime      string `json:"buildTime"`
	UpdateAvailable bool  `json:"updateAvailable"`
	LatestVersion  string `json:"latestVersion,omitempty"`
	CheckedAt      string `json:"checkedAt"`
}

// handleVersion returns the current version info and checks for updates
func handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	info := VersionInfo{
		Version:   buildVersion,
		BuildTime: buildTime,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Try to check for updates from GitHub (non-blocking, fail gracefully)
	if buildVersion != "dev" {
		latest, err := fetchLatestVersion()
		if err == nil && latest != "" && latest != buildVersion {
			info.UpdateAvailable = true
			info.LatestVersion = latest
		}
	}

	jsonResp(w, info)
}

// handleCheckUpdate is a lightweight endpoint that only checks for updates
func handleCheckUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	latest, err := fetchLatestVersion()
	result := map[string]interface{}{
		"currentVersion":  buildVersion,
		"updateAvailable": false,
		"checkedAt":       time.Now().UTC().Format(time.RFC3339),
	}

	if err != nil {
		result["error"] = err.Error()
	} else {
		result["latestVersion"] = latest
		if buildVersion != "dev" && latest != "" && latest != buildVersion {
			result["updateAvailable"] = true
		}
	}

	jsonResp(w, result)
}

// fetchLatestVersion queries the GitHub API for the latest commit SHA on master
func fetchLatestVersion() (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/refs/heads/master",
		gitRepoOwner, gitRepoName)

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	// No auth needed for public repos, but adding a User-Agent is required
	req.Header.Set("User-Agent", "spive2d-update-checker")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var result struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode error: %w", err)
	}

	// Return short SHA (first 7 characters)
	sha := result.Object.SHA
	if len(sha) > 7 {
		sha = sha[:7]
	}
	return sha, nil
}

// versionShort returns a human-readable version string
func versionShort() string {
	if buildVersion == "dev" {
		return "dev"
	}
	parts := []string{buildVersion}
	if buildTime != "unknown" {
		// Try to parse and reformat build time
		if t, err := time.Parse(time.RFC3339, buildTime); err == nil {
			parts = append(parts, t.Format("2006-01-02"))
		} else {
			parts = append(parts, buildTime)
		}
	}
	return strings.Join(parts, " ")
}
