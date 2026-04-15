package main

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var (
	db             *sql.DB
	llamaServerURL string
	adminAPIKey    string
)

type User struct {
	ID        int       `json:"id"`
	Username  string    `json:"username"`
	APIKey    string    `json:"api_key"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateUserRequest struct {
	Username string `json:"username"`
}

type CreateUserResponse struct {
	Username string `json:"username"`
	APIKey   string `json:"api_key"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func main() {
	// Get configuration from environment variables
	llamaServerURL = os.Getenv("LLAMA_SERVER_URL")
	if llamaServerURL == "" {
		llamaServerURL = "http://localhost:8080"
	}

	adminAPIKey = os.Getenv("ADMIN_API_KEY")
	if adminAPIKey == "" {
		adminAPIKey = generateAPIKey()
		log.Printf("Generated admin API key: %s", adminAPIKey)
		log.Println("Set ADMIN_API_KEY environment variable to use a custom key")
	}

	// Setup data directory
	dataPath := os.Getenv("DATA_PATH")
	if dataPath == "" {
		// Get executable directory
		execPath, err := os.Executable()
		if err != nil {
			log.Fatal("Failed to get executable path:", err)
		}
		execDir := filepath.Dir(execPath)
		dataPath = filepath.Join(execDir, "data")
	}

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(dataPath, 0755); err != nil {
		log.Fatal("Failed to create data directory:", err)
	}

	// Database path is inside the data directory
	dbPath := filepath.Join(dataPath, "main.db")

	// Initialize database
	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	if err := initDB(); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}

	// Setup routes
	http.HandleFunc("/admin/users", adminAuthMiddleware(handleCreateUser))
	http.HandleFunc("/admin/users/list", adminAuthMiddleware(handleListUsers))
	http.HandleFunc("/v1/", userAuthMiddleware(handleProxy))
	http.HandleFunc("/", userAuthMiddleware(handleProxy))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	log.Printf("Starting proxy server on port %s", port)
	log.Printf("Proxying to llama-server at: %s", llamaServerURL)
	log.Printf("Data directory: %s", dataPath)
	log.Printf("Database: %s", dbPath)
	log.Printf("Admin API Key: %s", adminAPIKey)

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal("Server failed:", err)
	}
}

func initDB() error {
	// Create users table
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			api_key TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create users table: %w", err)
	}

	// Create request_logs table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS request_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			username TEXT NOT NULL,
			ip_address TEXT NOT NULL,
			method TEXT NOT NULL,
			path TEXT NOT NULL,
			request_body TEXT,
			response_body TEXT,
			status_code INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create request_logs table: %w", err)
	}

	// Create indexes for better performance
	db.Exec("CREATE INDEX IF NOT EXISTS idx_users_api_key ON users(api_key)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_request_logs_user_id ON request_logs(user_id)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_request_logs_created_at ON request_logs(created_at)")

	return nil
}

func generateAPIKey() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		log.Fatal("Failed to generate API key:", err)
	}
	return "sk-" + hex.EncodeToString(bytes)
}

func adminAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "Missing authorization header"})
			return
		}

		// Support "Bearer <token>" format
		token := strings.TrimPrefix(authHeader, "Bearer ")
		token = strings.TrimSpace(token)

		if token != adminAPIKey {
			respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "Invalid admin API key"})
			return
		}

		next(w, r)
	}
}

func userAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "Missing authorization header"})
			return
		}

		// Support "Bearer <token>" format
		token := strings.TrimPrefix(authHeader, "Bearer ")
		token = strings.TrimSpace(token)

		// Verify API key
		var user User
		err := db.QueryRow("SELECT id, username, api_key, created_at FROM users WHERE api_key = ?", token).
			Scan(&user.ID, &user.Username, &user.APIKey, &user.CreatedAt)

		if err == sql.ErrNoRows {
			respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "Invalid API key"})
			return
		} else if err != nil {
			log.Printf("Database error: %v", err)
			respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Internal server error"})
			return
		}

		// Store user info in request headers for use in handler
		r.Header.Set("X-User-ID", fmt.Sprintf("%d", user.ID))
		r.Header.Set("X-Username", user.Username)

		next(w, r)
	}
}

func handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "Method not allowed"})
		return
	}

	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Invalid request body"})
		return
	}

	if req.Username == "" {
		respondJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Username is required"})
		return
	}

	// Generate API key
	apiKey := generateAPIKey()

	// Insert user into database
	_, err := db.Exec("INSERT INTO users (username, api_key) VALUES (?, ?)", req.Username, apiKey)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			respondJSON(w, http.StatusConflict, ErrorResponse{Error: "Username already exists"})
			return
		}
		log.Printf("Database error: %v", err)
		respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to create user"})
		return
	}

	log.Printf("Created user: %s with API key: %s", req.Username, apiKey)

	respondJSON(w, http.StatusCreated, CreateUserResponse{
		Username: req.Username,
		APIKey:   apiKey,
	})
}

func handleListUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "Method not allowed"})
		return
	}

	rows, err := db.Query("SELECT id, username, api_key, created_at FROM users ORDER BY created_at DESC")
	if err != nil {
		log.Printf("Database error: %v", err)
		respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to list users"})
		return
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Username, &user.APIKey, &user.CreatedAt); err != nil {
			log.Printf("Scan error: %v", err)
			continue
		}
		users = append(users, user)
	}

	if users == nil {
		users = []User{}
	}

	respondJSON(w, http.StatusOK, users)
}

func handleProxy(w http.ResponseWriter, r *http.Request) {
	// Get user info from headers (set by middleware)
	userID := r.Header.Get("X-User-ID")
	username := r.Header.Get("X-Username")
	ipAddress := getClientIP(r)

	// Read request body
	var requestBody []byte
	if r.Body != nil {
		var err error
		requestBody, err = io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading request body: %v", err)
			respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to read request"})
			return
		}
		r.Body.Close()
	}

	// Create proxy request
	targetURL := llamaServerURL + r.URL.Path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewBuffer(requestBody))
	if err != nil {
		log.Printf("Error creating proxy request: %v", err)
		respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to create proxy request"})
		return
	}

	// Copy headers (except Authorization and internal headers)
	for key, values := range r.Header {
		if key != "Authorization" && !strings.HasPrefix(key, "X-User") {
			for _, value := range values {
				proxyReq.Header.Add(key, value)
			}
		}
	}

	// Execute proxy request
	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("Error executing proxy request: %v", err)
		respondJSON(w, http.StatusBadGateway, ErrorResponse{Error: "Failed to reach llama-server"})

		// Log failed request
		go logRequest(userID, username, ipAddress, r.Method, r.URL.Path, requestBody, []byte(err.Error()), 502)
		return
	}
	defer resp.Body.Close()

	// Read response body
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body: %v", err)
		respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to read response"})
		return
	}

	// Log request asynchronously
	go logRequest(userID, username, ipAddress, r.Method, r.URL.Path, requestBody, responseBody, resp.StatusCode)

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Write response
	w.WriteHeader(resp.StatusCode)
	w.Write(responseBody)

	log.Printf("Proxied request: user=%s ip=%s method=%s path=%s status=%d", username, ipAddress, r.Method, r.URL.Path, resp.StatusCode)
}

func logRequest(userID, username, ipAddress, method, path string, requestBody, responseBody []byte, statusCode int) {
	_, err := db.Exec(`
		INSERT INTO request_logs (user_id, username, ip_address, method, path, request_body, response_body, status_code)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, username, ipAddress, method, path, string(requestBody), string(responseBody), statusCode)

	if err != nil {
		log.Printf("Error logging request: %v", err)
	}
}

func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxies)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
