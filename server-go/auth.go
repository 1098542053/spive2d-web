package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Role     string `json:"role"` // "admin" or "user"
}

type StoredUser struct {
	User
	Password string `json:"password"`
}

type Session struct {
	Token     string
	UserID    string
	ExpiresAt time.Time
}

var (
	sessions     = map[string]Session{}
	sessionsMu   sync.RWMutex
	storedUsers  = map[string]StoredUser{} // key = username (lowercase)
	storedUsersMu sync.RWMutex
	nextUserID   = 1
	usersMu      sync.Mutex
	usersFilePath string
)

func initAuth() {
	// Determine users file path
	dir := getEnv("DATA_DIR", "/data")
	usersFilePath = filepath.Join(dir, "users.json")
	loadUsers()
}

func loadUsers() {
	storedUsersMu.Lock()
	defer storedUsersMu.Unlock()

	data, err := os.ReadFile(usersFilePath)
	if err != nil {
		log.Printf("loadUsers: no users file yet (%v)", err)
		return
	}
	var list []StoredUser
	if err := json.Unmarshal(data, &list); err != nil {
		log.Printf("loadUsers: failed to parse users file (%v)", err)
		return
	}
	usersMu.Lock()
	defer usersMu.Unlock()
	for _, u := range list {
		storedUsers[strings.ToLower(u.Username)] = u
		id := 0
		if _, err := fmt.Sscanf(u.ID, "%d", &id); err == nil && id >= nextUserID {
			nextUserID = id + 1
		}
	}
}

// saveUsersLocked saves users to disk.
// The caller MUST already hold at least a read lock on storedUsersMu.
func saveUsersLocked() {
	list := make([]StoredUser, 0, len(storedUsers))
	for _, u := range storedUsers {
		list = append(list, u)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].ID < list[j].ID
	})

	data, _ := json.MarshalIndent(list, "", "  ")
	os.MkdirAll(filepath.Dir(usersFilePath), 0755)
	os.WriteFile(usersFilePath, data, 0644)
}

func saveUsers() {
	storedUsersMu.RLock()
	defer storedUsersMu.RUnlock()
	saveUsersLocked()
}

func needsSetup() bool {
	storedUsersMu.RLock()
	defer storedUsersMu.RUnlock()
	return len(storedUsers) == 0
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func getSessionUser(r *http.Request) *User {
	cookie, err := r.Cookie("session")
	if err != nil {
		return nil
	}
	sessionsMu.RLock()
	s, ok := sessions[cookie.Value]
	sessionsMu.RUnlock()
	if !ok || time.Now().After(s.ExpiresAt) {
		return nil
	}

	storedUsersMu.RLock()
	su, ok := storedUsers[s.UserID]
	storedUsersMu.RUnlock()
	if !ok {
		return nil
	}
	return &User{
		ID:       su.ID,
		Username: su.Username,
		Nickname: su.Nickname,
		Role:     su.Role,
	}
}

// --- Handlers ---

func handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Nickname string `json:"nickname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || body.Password == "" {
		jsonError(w, "Username and password are required", http.StatusBadRequest)
		return
	}
	if len(body.Password) < 4 {
		jsonError(w, "Password must be at least 4 characters", http.StatusBadRequest)
		return
	}

	key := strings.ToLower(body.Username)

	storedUsersMu.Lock()
	defer storedUsersMu.Unlock()

	if _, exists := storedUsers[key]; exists {
		jsonError(w, "Username already exists", http.StatusConflict)
		return
	}

	isFirst := len(storedUsers) == 0
	role := "user"
	if isFirst {
		role = "admin"
	}

	usersMu.Lock()
	id := nextUserID
	nextUserID++
	usersMu.Unlock()

	nickname := body.Nickname
	if nickname == "" {
		nickname = body.Username
	}

	user := StoredUser{
		User: User{
			ID:       fmt.Sprintf("%d", id),
			Username: body.Username,
			Nickname: nickname,
			Role:     role,
		},
		Password: body.Password,
	}
	storedUsers[key] = user
	saveUsersLocked() // already holding storedUsersMu write lock

	// Auto-login after register
	token := generateToken()
	sessionsMu.Lock()
	sessions[token] = Session{
		Token:     token,
		UserID:    key,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	sessionsMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	jsonResp(w, map[string]interface{}{
		"user":    user.User,
		"message": "注册成功，欢迎 " + nickname + "！" + map[bool]string{true: " 你已自动成为管理员", false: ""}[isFirst],
	})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		RememberMe bool   `json:"rememberMe"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	key := strings.ToLower(strings.TrimSpace(body.Username))

	storedUsersMu.RLock()
	su, ok := storedUsers[key]
	storedUsersMu.RUnlock()

	if !ok || su.Password != body.Password {
		jsonError(w, "用户名或密码错误", http.StatusUnauthorized)
		return
	}

	// Session duration: 30 days if rememberMe, otherwise 24 hours
	sessionDuration := 24 * time.Hour
	cookieMaxAge := 86400
	if body.RememberMe {
		sessionDuration = 30 * 24 * time.Hour
		cookieMaxAge = 30 * 86400
	}

	token := generateToken()
	sessionsMu.Lock()
	sessions[token] = Session{
		Token:     token,
		UserID:    key,
		ExpiresAt: time.Now().Add(sessionDuration),
	}
	sessionsMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		MaxAge:   cookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	jsonResp(w, map[string]interface{}{"user": su.User})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		sessionsMu.Lock()
		delete(sessions, cookie.Value)
		sessionsMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	jsonResp(w, map[string]string{"status": "ok"})
}

func handleMe(w http.ResponseWriter, r *http.Request) {
	if needsSetup() {
		jsonResp(w, map[string]interface{}{
			"user":       nil,
			"needsSetup": true,
		})
		return
	}

	user := getSessionUser(r)
	if user == nil {
		// Return needsSetup=false since there ARE users, just not logged in
		jsonResp(w, map[string]interface{}{
			"user":       nil,
			"needsSetup": false,
		})
		return
	}
	jsonResp(w, map[string]interface{}{
		"user":       user,
		"needsSetup": false,
	})
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
