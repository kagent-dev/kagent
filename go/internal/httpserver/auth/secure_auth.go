package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/pkg/auth"
)

// JWT-like token structure for demonstration
type Token struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
	IssuedAt  time.Time `json:"issued_at"`
}

type SecureAuthenticator struct {
	// In production, this would be a database or cache
	userStore map[string]User
	tokenStore map[string]Token
}

type User struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Password string `json:"-"` // Never serialize password
}

func NewSecureAuthenticator() *SecureAuthenticator {
	// Mock user store - in production, this would be a database
	users := map[string]User{
		"1": {
			ID:       "1",
			Email:    "admin@kagent.dev",
			Name:     "Admin User",
			Role:     "admin",
			Password: "admin123", // In production, this would be hashed
		},
		"2": {
			ID:       "2",
			Email:    "user@kagent.dev",
			Name:     "Regular User",
			Role:     "user",
			Password: "user123",
		},
	}

	return &SecureAuthenticator{
		userStore:  users,
		tokenStore: make(map[string]Token),
	}
}

func (a *SecureAuthenticator) Login(email, password string) (*User, string, error) {
	// Find user by email
	var user *User
	for _, u := range a.userStore {
		if u.Email == email {
			user = &u
			break
		}
	}

	if user == nil || user.Password != password {
		return nil, "", fmt.Errorf("invalid credentials")
	}

	// Generate token
	token := Token{
		UserID:    user.ID,
		Email:     user.Email,
		Name:      user.Name,
		Role:      user.Role,
		ExpiresAt: time.Now().Add(24 * time.Hour), // 24 hour expiry
		IssuedAt:  time.Now(),
	}

	// Generate random token string
	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	tokenString := base64.URLEncoding.EncodeToString(tokenBytes)

	// Store token
	a.tokenStore[tokenString] = token

	return user, tokenString, nil
}

func (a *SecureAuthenticator) ValidateToken(tokenString string) (*User, error) {
	token, exists := a.tokenStore[tokenString]
	if !exists {
		return nil, fmt.Errorf("invalid token")
	}

	// Check if token is expired
	if time.Now().After(token.ExpiresAt) {
		delete(a.tokenStore, tokenString) // Clean up expired token
		return nil, fmt.Errorf("token expired")
	}

	// Get user from store
	user, exists := a.userStore[token.UserID]
	if !exists {
		return nil, fmt.Errorf("user not found")
	}

	return &user, nil
}

func (a *SecureAuthenticator) Logout(tokenString string) {
	delete(a.tokenStore, tokenString)
}

// HTTP handlers
func (a *SecureAuthenticator) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var loginReq struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&loginReq); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	user, token, err := a.Login(loginReq.Email, loginReq.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	response := map[string]interface{}{
		"user":  user,
		"token": token,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (a *SecureAuthenticator) MeHandler(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Authorization header required", http.StatusUnauthorized)
		return
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	user, err := a.ValidateToken(tokenString)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (a *SecureAuthenticator) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Authorization header required", http.StatusUnauthorized)
		return
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	a.Logout(tokenString)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Logged out successfully"})
}

// Implement AuthProvider interface
func (a *SecureAuthenticator) Authenticate(ctx context.Context, reqHeaders http.Header, query url.Values) (auth.Session, error) {
	authHeader := reqHeaders.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		// Return a default session for unauthenticated requests
		return &SimpleSession{
			P: auth.Principal{
				User: auth.User{
					ID: "anonymous",
				},
			},
		}, nil
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	user, err := a.ValidateToken(tokenString)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	return &SimpleSession{
		P: auth.Principal{
			User: auth.User{
				ID:    user.ID,
				Roles: []string{user.Role},
			},
		},
	}, nil
}

func (a *SecureAuthenticator) UpstreamAuth(r *http.Request, session auth.Session, upstreamPrincipal auth.Principal) error {
	if session == nil || session.Principal().User.ID == "" {
		return nil
	}
	
	// Add user ID to upstream request headers
	r.Header.Set("X-User-Id", session.Principal().User.ID)
	r.Header.Set("X-User-Roles", strings.Join(session.Principal().User.Roles, ","))
	
	return nil
}

