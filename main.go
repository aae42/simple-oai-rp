package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
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
	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (OWASP recommended)
const (
	argon2Time    = 2
	argon2Memory  = 19 * 1024 // 19 MiB
	argon2Threads = 1
	argon2KeyLen  = 32
	argon2SaltLen = 16
)

// API key prefix length for fast lookup (unhashed portion)
// Format: prefix$argon2id$...
const apiKeyPrefixLen = 8

var (
	db               *sql.DB
	llamaServerURL   string
	adminAPIKeyHash  string
)

type User struct {
	ID         int       `json:"id"`
	Username   string    `json:"username"`
	APIKeyHash string    `json:"api_key_hash,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
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
	llamaServerURL = os.Getenv("SIMPLE_LLAMA_SERVER_URL")
	if llamaServerURL == "" {
		llamaServerURL = "http://localhost:8080"
	}

	adminAPIKeyHash = os.Getenv("SIMPLE_ADMIN_API_KEY_HASH")
	if adminAPIKeyHash == "" {
		// Generate a new admin API key and hash it
		adminAPIKey := generateAPIKey()
		var err error
		adminAPIKeyHash, err = hashAPIKey(adminAPIKey)
		if err != nil {
			log.Fatal("Failed to hash admin API key:", err)
		}
		log.Printf("Generated admin API key: %s", adminAPIKey)
		log.Printf("Admin API key hash: %s", adminAPIKeyHash)
		log.Println("Set SIMPLE_ADMIN_API_KEY_HASH environment variable to use a custom key hash")
	}

	// Setup data directory
	dataPath := os.Getenv("SIMPLE_DATA_PATH")
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

	// Database migrations are handled by dbmate
	// Run: just db-migrate

	// Setup routes
	http.HandleFunc("/admin/users", adminAuthMiddleware(handleCreateUser))
	http.HandleFunc("/admin/users/list", adminAuthMiddleware(handleListUsers))
	http.HandleFunc("/v1/", userAuthMiddleware(handleProxy))
	http.HandleFunc("/", userAuthMiddleware(handleProxy))

	port := os.Getenv("SIMPLE_PORT")
	if port == "" {
		port = "8081"
	}

	log.Printf("Starting proxy server on port %s", port)
	log.Printf("Proxying to llama-server at: %s", llamaServerURL)
	log.Printf("Data directory: %s", dataPath)
	log.Printf("Database: %s", dbPath)

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal("Server failed:", err)
	}
}

func generateAPIKey() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		log.Fatal("Failed to generate API key:", err)
	}
	return "sk-" + hex.EncodeToString(bytes)
}

// hashAPIKey creates an argon2id hash of the API key with an unhashed prefix for fast lookup
// Returns the hash in the format: <prefix>$argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>
// The prefix is the first apiKeyPrefixLen characters of the API key (unhashed)
func hashAPIKey(apiKey string) (string, error) {
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(apiKey), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)

	// Encode in PHC string format with prefix
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	// Extract prefix from API key (e.g., "sk-abc12" from "sk-abc123...")
	prefix := apiKey
	if len(apiKey) > apiKeyPrefixLen {
		prefix = apiKey[:apiKeyPrefixLen]
	}

	return fmt.Sprintf("%s$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		prefix, argon2.Version, argon2Memory, argon2Time, argon2Threads, b64Salt, b64Hash), nil
}

// extractPrefix extracts the unhashed prefix from a stored hash
func extractPrefix(encodedHash string) string {
	// Format: <prefix>$argon2id$...
	idx := strings.Index(encodedHash, "$argon2id$")
	if idx == -1 {
		return ""
	}
	return encodedHash[:idx]
}

// verifyAPIKey verifies an API key against an argon2id hash
// Format: <prefix>$argon2id$v=X$m=X,t=X,p=X$salt$hash (6 parts when split by $)
func verifyAPIKey(apiKey, encodedHash string) (bool, error) {
	// Parse the PHC string format with prefix
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false, fmt.Errorf("invalid hash format: expected 6 parts, got %d", len(parts))
	}

	// parts[0] is the prefix, parts[1] is "argon2id", etc.
	storedPrefix := parts[0]
	if parts[1] != "argon2id" {
		return false, fmt.Errorf("invalid algorithm: %s", parts[1])
	}

	// Quick prefix check before expensive hash verification
	prefix := apiKey
	if len(apiKey) > apiKeyPrefixLen {
		prefix = apiKey[:apiKeyPrefixLen]
	}
	if storedPrefix != prefix {
		return false, nil
	}

	var version int
	_, err := fmt.Sscanf(parts[2], "v=%d", &version)
	if err != nil {
		return false, fmt.Errorf("invalid version: %w", err)
	}

	var memory, time uint32
	var threads uint8
	_, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads)
	if err != nil {
		return false, fmt.Errorf("invalid parameters: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("invalid salt: %w", err)
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("invalid hash: %w", err)
	}

	// Compute hash with same parameters
	computedHash := argon2.IDKey([]byte(apiKey), salt, time, memory, threads, uint32(len(expectedHash)))

	// Constant-time comparison
	return subtle.ConstantTimeCompare(computedHash, expectedHash) == 1, nil
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

		valid, err := verifyAPIKey(token, adminAPIKeyHash)
		if err != nil {
			log.Printf("Error verifying admin API key: %v", err)
			respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "Invalid admin API key"})
			return
		}

		if !valid {
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

		// Extract prefix from the API key for fast lookup
		prefix := token
		if len(token) > apiKeyPrefixLen {
			prefix = token[:apiKeyPrefixLen]
		}

		// Query only users whose hash starts with the same prefix
		rows, err := db.Query("SELECT id, username, api_key_hash, created_at FROM users WHERE api_key_hash LIKE ?", prefix+"%")
		if err != nil {
			log.Printf("Database error: %v", err)
			respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Internal server error"})
			return
		}
		defer rows.Close()

		var authenticatedUser *User
		for rows.Next() {
			var user User
			if err := rows.Scan(&user.ID, &user.Username, &user.APIKeyHash, &user.CreatedAt); err != nil {
				log.Printf("Scan error: %v", err)
				continue
			}

			valid, err := verifyAPIKey(token, user.APIKeyHash)
			if err != nil {
				log.Printf("Error verifying API key for user %s: %v", user.Username, err)
				continue
			}

			if valid {
				authenticatedUser = &user
				break
			}
		}

		if authenticatedUser == nil {
			respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "Invalid API key"})
			return
		}

		// Store user info in request headers for use in handler
		r.Header.Set("X-User-ID", fmt.Sprintf("%d", authenticatedUser.ID))
		r.Header.Set("X-Username", authenticatedUser.Username)

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

	// Generate API key and hash it
	apiKey := generateAPIKey()
	apiKeyHash, err := hashAPIKey(apiKey)
	if err != nil {
		log.Printf("Error hashing API key: %v", err)
		respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to create user"})
		return
	}

	// Insert user into database with hashed API key
	_, err = db.Exec("INSERT INTO users (username, api_key_hash) VALUES (?, ?)", req.Username, apiKeyHash)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			respondJSON(w, http.StatusConflict, ErrorResponse{Error: "Username already exists"})
			return
		}
		log.Printf("Database error: %v", err)
		respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to create user"})
		return
	}

	log.Printf("Created user: %s", req.Username)

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

	rows, err := db.Query("SELECT id, username, created_at FROM users ORDER BY created_at DESC")
	if err != nil {
		log.Printf("Database error: %v", err)
		respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to list users"})
		return
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Username, &user.CreatedAt); err != nil {
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

// isStreamingRequest checks if the request body contains "stream": true
func isStreamingRequest(body []byte) bool {
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}
	if stream, ok := req["stream"]; ok {
		if streamBool, ok := stream.(bool); ok {
			return streamBool
		}
	}
	return false
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

	// Check if this is a streaming request
	isStreaming := isStreamingRequest(requestBody)

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

	// Execute proxy request - use a transport without timeout for streaming
	var client *http.Client
	if isStreaming {
		// For streaming requests, don't set a timeout on the client
		// The connection will stay open until the stream completes
		client = &http.Client{}
	} else {
		client = &http.Client{Timeout: 300 * time.Second}
	}

	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("Error executing proxy request: %v", err)
		respondJSON(w, http.StatusBadGateway, ErrorResponse{Error: "Failed to reach llama-server"})

		// Log failed request
		go logRequest(userID, username, ipAddress, r.Method, r.URL.Path, requestBody, []byte(err.Error()), 502)
		return
	}
	defer resp.Body.Close()

	// Handle streaming response
	if isStreaming {
		handleStreamingResponse(w, resp, userID, username, ipAddress, r.Method, r.URL.Path, requestBody)
		return
	}

	// Non-streaming: read entire response body
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

func handleStreamingResponse(w http.ResponseWriter, resp *http.Response, userID, username, ipAddress, method, path string, requestBody []byte) {
	// Check if we can flush (required for streaming)
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("ResponseWriter does not support flushing, falling back to buffered response")
		// Fall back to buffered response
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading response body: %v", err)
			respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to read response"})
			return
		}
		go logRequest(userID, username, ipAddress, method, path, requestBody, responseBody, resp.StatusCode)
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(responseBody)
		return
	}

	// Copy response headers for SSE
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Ensure proper headers for SSE streaming
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering if present

	w.WriteHeader(resp.StatusCode)

	// Buffer to collect the full response for logging
	var responseBuffer bytes.Buffer

	// Use a buffered reader to read line by line (SSE format)
	reader := bufio.NewReader(resp.Body)

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			// Write to response buffer for logging
			responseBuffer.Write(line)

			// Write to client
			_, writeErr := w.Write(line)
			if writeErr != nil {
				log.Printf("Error writing streaming response: %v", writeErr)
				break
			}

			// Flush immediately to send the chunk to the client
			flusher.Flush()
		}

		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading streaming response: %v", err)
			}
			break
		}
	}

	// Log the complete streamed response asynchronously
	go logRequest(userID, username, ipAddress, method, path, requestBody, responseBuffer.Bytes(), resp.StatusCode)

	log.Printf("Proxied streaming request: user=%s ip=%s method=%s path=%s status=%d", username, ipAddress, method, path, resp.StatusCode)
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
