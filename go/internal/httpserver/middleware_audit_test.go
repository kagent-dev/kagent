package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kagent-dev/kagent/go/pkg/auth"
)

// mockSession implements auth.Session for testing
type mockSession struct {
	principal auth.Principal
}

func (m *mockSession) Principal() auth.Principal {
	return m.principal
}

func TestAuditLoggingMiddleware_Disabled(t *testing.T) {
	config := AuditLogConfig{
		Enabled: false,
	}

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := auditLoggingMiddleware(config)
	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/agents/default/test", nil)
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	if !handlerCalled {
		t.Error("Expected handler to be called when audit logging is disabled")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestAuditLoggingMiddleware_Enabled(t *testing.T) {
	config := AuditLogConfig{
		Enabled:  true,
		LogLevel: 0,
	}

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusCreated)
	})

	middleware := auditLoggingMiddleware(config)
	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodPost, "/api/agents/my-namespace/my-agent", nil)
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	if !handlerCalled {
		t.Error("Expected handler to be called when audit logging is enabled")
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", rec.Code)
	}
}

func TestAuditLoggingMiddleware_WithAuthenticatedUser(t *testing.T) {
	config := AuditLogConfig{
		Enabled:  true,
		LogLevel: 0,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := auditLoggingMiddleware(config)
	wrappedHandler := middleware(handler)

	// Create request with auth session in context
	req := httptest.NewRequest(http.MethodGet, "/api/agents/test-ns/test-agent", nil)
	session := &mockSession{
		principal: auth.Principal{
			User: auth.User{
				ID:    "test-user-123",
				Roles: []string{"admin", "user"},
			},
		},
	}
	ctx := auth.AuthSessionTo(req.Context(), session)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestAuditLoggingMiddleware_WithRequestID(t *testing.T) {
	config := AuditLogConfig{
		Enabled:  true,
		LogLevel: 0,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := auditLoggingMiddleware(config)
	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("X-Request-ID", "custom-request-id-12345")
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestAuditLoggingMiddleware_WithIncludeHeaders(t *testing.T) {
	config := AuditLogConfig{
		Enabled:        true,
		LogLevel:       0,
		IncludeHeaders: []string{"X-Correlation-ID", "X-Trace-ID"},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := auditLoggingMiddleware(config)
	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("X-Correlation-ID", "corr-12345")
	req.Header.Set("X-Trace-ID", "trace-67890")
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestExtractNamespace(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		query    string
		header   string
		expected string
	}{
		{
			name:     "extract from path - agents",
			path:     "/api/agents/my-namespace/my-agent",
			expected: "my-namespace",
		},
		{
			name:     "extract from path - sessions",
			path:     "/api/sessions/agent/test-ns/test-agent",
			expected: "agent", // The first segment after /api/sessions/
		},
		{
			name:     "extract from path - tools",
			path:     "/api/tools/production/my-tool",
			expected: "production",
		},
		{
			name:     "extract from query parameter",
			path:     "/api/agents",
			query:    "namespace=query-ns",
			expected: "query-ns",
		},
		{
			name:     "extract from header",
			path:     "/api/agents",
			header:   "header-ns",
			expected: "header-ns",
		},
		{
			name:     "fallback to unknown",
			path:     "/api/agents",
			expected: "unknown",
		},
		{
			name:     "path takes precedence over query",
			path:     "/api/agents/path-ns/agent",
			query:    "namespace=query-ns",
			expected: "path-ns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := tt.path
			if tt.query != "" {
				url += "?" + tt.query
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			if tt.header != "" {
				req.Header.Set("X-Namespace", tt.header)
			}

			result := extractNamespace(req)

			if result != tt.expected {
				t.Errorf("extractNamespace() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestCategorizeResult(t *testing.T) {
	tests := []struct {
		status   int
		expected string
	}{
		{200, "success"},
		{201, "success"},
		{204, "success"},
		{299, "success"},
		{301, "redirect"},
		{302, "redirect"},
		{304, "redirect"},
		{400, "client_error"},
		{401, "client_error"},
		{403, "client_error"},
		{404, "client_error"},
		{422, "client_error"},
		{500, "server_error"},
		{502, "server_error"},
		{503, "server_error"},
		{100, "unknown"},
		{199, "unknown"},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			result := categorizeResult(tt.status)
			if result != tt.expected {
				t.Errorf("categorizeResult(%d) = %q, want %q", tt.status, result, tt.expected)
			}
		})
	}
}

func TestDefaultAuditLogConfig(t *testing.T) {
	// Test default config (without env var set)
	config := DefaultAuditLogConfig()

	if !config.Enabled {
		t.Error("Expected audit logging to be enabled by default")
	}
	if config.LogLevel != 0 {
		t.Errorf("Expected LogLevel 0, got %d", config.LogLevel)
	}
	if len(config.IncludeHeaders) != 0 {
		t.Errorf("Expected empty IncludeHeaders, got %v", config.IncludeHeaders)
	}
}

func TestAuditLoggingMiddleware_ErrorStatus(t *testing.T) {
	config := AuditLogConfig{
		Enabled:  true,
		LogLevel: 0,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	middleware := auditLoggingMiddleware(config)
	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rec.Code)
	}
}

func TestAuditLoggingMiddleware_AnonymousUser(t *testing.T) {
	config := AuditLogConfig{
		Enabled:  true,
		LogLevel: 0,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := auditLoggingMiddleware(config)
	wrappedHandler := middleware(handler)

	// Request without auth session
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

// TestAuditLoggingMiddleware_AllHTTPMethods tests that all HTTP methods are logged
func TestAuditLoggingMiddleware_AllHTTPMethods(t *testing.T) {
	config := AuditLogConfig{
		Enabled:  true,
		LogLevel: 0,
	}

	methods := []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			middleware := auditLoggingMiddleware(config)
			wrappedHandler := middleware(handler)

			req := httptest.NewRequest(method, "/api/agents/ns/agent", nil)
			rec := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Expected status 200 for %s, got %d", method, rec.Code)
			}
		})
	}
}
