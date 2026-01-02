package httpserver

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// AuditLogConfig holds configuration for the audit logging middleware
type AuditLogConfig struct {
	// Enabled controls whether audit logging is active
	Enabled bool
	// LogLevel controls the verbosity level (0=Info, 1+=Debug)
	LogLevel int
	// IncludeHeaders specifies which headers to include in audit logs (for compliance)
	IncludeHeaders []string
}

// DefaultAuditLogConfig returns the default audit logging configuration
func DefaultAuditLogConfig() AuditLogConfig {
	enabled := os.Getenv("KAGENT_AUDIT_LOG_ENABLED") != "false"
	return AuditLogConfig{
		Enabled:        enabled,
		LogLevel:       0,
		IncludeHeaders: []string{},
	}
}

// requestIDKey is the context key for request ID
type requestIDKey struct{}

// namespacePattern matches namespace in API paths like /api/agents/{namespace}/{name}
var namespacePattern = regexp.MustCompile(`^/api/[^/]+/([^/]+)(?:/|$)`)

// auditLoggingMiddleware creates a middleware for structured audit logging
// It logs compliance-ready audit trail with user, namespace, action, result, and duration
func auditLoggingMiddleware(config AuditLogConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()

			// Generate or extract request ID for correlation
			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = uuid.New().String()
			}

			// Extract user information from auth session
			userID := "anonymous"
			userRoles := []string{}
			if session, ok := auth.AuthSessionFrom(r.Context()); ok && session != nil {
				principal := session.Principal()
				if principal.User.ID != "" {
					userID = principal.User.ID
				}
				userRoles = principal.User.Roles
			}

			// Extract namespace from path or header
			namespace := extractNamespace(r)

			// Build action string (HTTP method + path pattern)
			action := r.Method + " " + r.URL.Path

			// Create audit logger with structured fields
			auditLog := ctrllog.Log.WithName("audit").WithValues(
				"request_id", requestID,
				"timestamp", start.UTC().Format(time.RFC3339Nano),
				"user", userID,
				"user_roles", userRoles,
				"namespace", namespace,
				"action", action,
				"method", r.Method,
				"path", r.URL.Path,
				"remote_addr", r.RemoteAddr,
				"user_agent", r.Header.Get("User-Agent"),
			)

			// Include specified headers for compliance if configured
			for _, header := range config.IncludeHeaders {
				if val := r.Header.Get(header); val != "" {
					auditLog = auditLog.WithValues("header_"+strings.ToLower(strings.ReplaceAll(header, "-", "_")), val)
				}
			}

			// Wrap response writer to capture status code
			ww := newStatusResponseWriter(w)

			// Log request start at configured level
			auditLog.V(config.LogLevel).Info("Audit: request started")

			// Serve the request
			next.ServeHTTP(ww, r)

			// Calculate duration
			duration := time.Since(start)

			// Determine result category for compliance
			resultCategory := categorizeResult(ww.status)

			// Log request completion with full audit trail
			auditLog.Info("Audit: request completed",
				"status", ww.status,
				"result", resultCategory,
				"duration_ms", duration.Milliseconds(),
				"duration", duration.String(),
			)
		})
	}
}

// extractNamespace extracts the namespace from the request path or headers
func extractNamespace(r *http.Request) string {
	// First, try to extract from the URL path pattern
	// Patterns like /api/agents/{namespace}/{name} or /api/sessions/agent/{namespace}/{name}
	matches := namespacePattern.FindStringSubmatch(r.URL.Path)
	if len(matches) > 1 {
		return matches[1]
	}

	// Try query parameter
	if ns := r.URL.Query().Get("namespace"); ns != "" {
		return ns
	}

	// Try header
	if ns := r.Header.Get("X-Namespace"); ns != "" {
		return ns
	}

	return "unknown"
}

// categorizeResult returns a human-readable result category for the status code
func categorizeResult(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "success"
	case status >= 300 && status < 400:
		return "redirect"
	case status >= 400 && status < 500:
		return "client_error"
	case status >= 500:
		return "server_error"
	default:
		return "unknown"
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		log := ctrllog.Log.WithName("http").WithValues(
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
		)

		if userID := r.URL.Query().Get("user_id"); userID != "" {
			log = log.WithValues("user_id", userID)
		}

		ww := newStatusResponseWriter(w)
		ctx := ctrllog.IntoContext(r.Context(), log)
		log.V(1).Info("Request started")
		next.ServeHTTP(ww, r.WithContext(ctx))
		log.Info("Request completed",
			"status", ww.status,
			"duration", time.Since(start),
		)
	})
}

// For streaming responses in A2A lib
var _ http.Flusher = &statusResponseWriter{}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func newStatusResponseWriter(w http.ResponseWriter) *statusResponseWriter {
	return &statusResponseWriter{w, http.StatusOK}
}

func (w *statusResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Forward RespondWithError to underlying writer if it implements ErrorResponseWriter
func (w *statusResponseWriter) RespondWithError(err error) {
	if errWriter, ok := w.ResponseWriter.(handlers.ErrorResponseWriter); ok {
		errWriter.RespondWithError(err)
		w.status = 500
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func contentTypeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
			w.Header().Set("Content-Type", "application/json")
		}
		next.ServeHTTP(w, r)
	})
}
