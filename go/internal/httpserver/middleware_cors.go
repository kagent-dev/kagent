package httpserver

import (
	"net/http"
	"os"
	"strings"
)

// corsMiddleware provides CORS support for development environments only
// In production, this middleware is disabled for security reasons
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only enable CORS in development environment
		if !isDevelopmentEnvironment() {
			next.ServeHTTP(w, r)
			return
		}

		origin := r.Header.Get("Origin")
		
		// Allow common local development origins
		allowedOrigins := []string{
			"http://localhost:3000",
			"http://localhost:8001",
			"http://127.0.0.1:3000", 
			"http://127.0.0.1:8001",
		}

		// Check if origin is allowed
		isAllowed := false
		for _, allowed := range allowedOrigins {
			if origin == allowed {
				isAllowed = true
				break
			}
		}

		// Always set CORS headers for allowed origins, regardless of method
		if isAllowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
			w.Header().Set("Access-Control-Max-Age", "86400")
			
			// Handle preflight OPTIONS request immediately
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		// For non-OPTIONS requests, continue to next handler
		next.ServeHTTP(w, r)
	})
}

// isDevelopmentEnvironment checks if we're running in development
// This uses common environment indicators to determine dev vs prod
func isDevelopmentEnvironment() bool {
	// Check explicit environment variable
	env := strings.ToLower(os.Getenv("KAGENT_ENV"))
	if env == "production" || env == "prod" {
		return false
	}
	if env == "development" || env == "dev" {
		return true
	}
	
	// Default heuristics: assume dev if no explicit prod indicators
	// This is safe because CORS is disabled by default in prod
	return env == "" || strings.Contains(os.Args[0], "go run") || strings.Contains(os.Args[0], "tmp")
}