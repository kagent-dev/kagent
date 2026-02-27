package handlers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	database_fake "github.com/kagent-dev/kagent/go/internal/database/fake"
	"github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
)

func newGitReposHandler(mcpURL string) *handlers.GitReposHandler {
	scheme := runtime.NewScheme()
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	dbClient := database_fake.NewClient()
	base := &handlers.Base{
		KubeClient:         kubeClient,
		DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default"},
		DatabaseService:    dbClient,
		Authorizer:         &auth.NoopAuthorizer{},
	}
	return handlers.NewGitReposHandler(base, mcpURL)
}

func TestGitReposHandler(t *testing.T) {
	t.Run("ServiceNotConfigured", func(t *testing.T) {
		handler := newGitReposHandler("")
		rr := newMockErrorResponseWriter()
		req := httptest.NewRequest("GET", "/api/gitrepos", nil)
		req = setUser(req, "test-user")

		handler.HandleListRepos(rr, req)

		assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
		var body map[string]string
		err := json.Unmarshal(rr.Body.Bytes(), &body)
		require.NoError(t, err)
		assert.Contains(t, body["error"], "not configured")
	})

	t.Run("ProxyListRepos", func(t *testing.T) {
		downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/repos", r.URL.Path)
			assert.Equal(t, http.MethodGet, r.Method)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[{"name":"test-repo","status":"indexed"}]`))
		}))
		defer downstream.Close()

		handler := newGitReposHandler(downstream.URL)
		rr := newMockErrorResponseWriter()
		req := httptest.NewRequest("GET", "/api/gitrepos", nil)
		req = setUser(req, "test-user")

		handler.HandleListRepos(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), "test-repo")
	})

	t.Run("ProxyAddRepo", func(t *testing.T) {
		downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/repos", r.URL.Path)
			assert.Equal(t, http.MethodPost, r.Method)
			body, _ := io.ReadAll(r.Body)
			assert.Contains(t, string(body), "my-repo")
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"name":"my-repo","status":"cloning"}`))
		}))
		defer downstream.Close()

		handler := newGitReposHandler(downstream.URL)
		rr := newMockErrorResponseWriter()
		req := httptest.NewRequest("POST", "/api/gitrepos", strings.NewReader(`{"name":"my-repo","url":"https://github.com/test/test.git"}`))
		req = setUser(req, "test-user")

		handler.HandleAddRepo(rr, req)

		assert.Equal(t, http.StatusAccepted, rr.Code)
	})

	t.Run("ProxyGetRepo", func(t *testing.T) {
		downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/repos/my-repo", r.URL.Path)
			assert.Equal(t, http.MethodGet, r.Method)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"name":"my-repo","status":"indexed"}`))
		}))
		defer downstream.Close()

		handler := newGitReposHandler(downstream.URL)
		rr := newMockErrorResponseWriter()
		req := httptest.NewRequest("GET", "/api/gitrepos/my-repo", nil)
		req = mux.SetURLVars(req, map[string]string{"name": "my-repo"})
		req = setUser(req, "test-user")

		handler.HandleGetRepo(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), "my-repo")
	})

	t.Run("ProxyDeleteRepo", func(t *testing.T) {
		downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/repos/my-repo", r.URL.Path)
			assert.Equal(t, http.MethodDelete, r.Method)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"message":"deleted"}`))
		}))
		defer downstream.Close()

		handler := newGitReposHandler(downstream.URL)
		rr := newMockErrorResponseWriter()
		req := httptest.NewRequest("DELETE", "/api/gitrepos/my-repo", nil)
		req = mux.SetURLVars(req, map[string]string{"name": "my-repo"})
		req = setUser(req, "test-user")

		handler.HandleDeleteRepo(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("ProxySyncRepo", func(t *testing.T) {
		downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/repos/my-repo/sync", r.URL.Path)
			assert.Equal(t, http.MethodPost, r.Method)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"message":"synced"}`))
		}))
		defer downstream.Close()

		handler := newGitReposHandler(downstream.URL)
		rr := newMockErrorResponseWriter()
		req := httptest.NewRequest("POST", "/api/gitrepos/my-repo/sync", nil)
		req = mux.SetURLVars(req, map[string]string{"name": "my-repo"})
		req = setUser(req, "test-user")

		handler.HandleSyncRepo(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("ProxyIndexRepo", func(t *testing.T) {
		downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/repos/my-repo/index", r.URL.Path)
			assert.Equal(t, http.MethodPost, r.Method)
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"message":"indexing started"}`))
		}))
		defer downstream.Close()

		handler := newGitReposHandler(downstream.URL)
		rr := newMockErrorResponseWriter()
		req := httptest.NewRequest("POST", "/api/gitrepos/my-repo/index", nil)
		req = mux.SetURLVars(req, map[string]string{"name": "my-repo"})
		req = setUser(req, "test-user")

		handler.HandleIndexRepo(rr, req)

		assert.Equal(t, http.StatusAccepted, rr.Code)
	})

	t.Run("ProxySearchRepo", func(t *testing.T) {
		downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/repos/my-repo/search", r.URL.Path)
			assert.Equal(t, http.MethodPost, r.Method)
			body, _ := io.ReadAll(r.Body)
			assert.Contains(t, string(body), "auth middleware")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[{"score":0.95,"filePath":"auth.go"}]`))
		}))
		defer downstream.Close()

		handler := newGitReposHandler(downstream.URL)
		rr := newMockErrorResponseWriter()
		req := httptest.NewRequest("POST", "/api/gitrepos/my-repo/search", strings.NewReader(`{"query":"auth middleware"}`))
		req = mux.SetURLVars(req, map[string]string{"name": "my-repo"})
		req = setUser(req, "test-user")

		handler.HandleSearchRepo(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), "auth.go")
	})

	t.Run("ProxySearchAll", func(t *testing.T) {
		downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/search", r.URL.Path)
			assert.Equal(t, http.MethodPost, r.Method)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[{"score":0.90,"repo":"repo1","filePath":"main.go"}]`))
		}))
		defer downstream.Close()

		handler := newGitReposHandler(downstream.URL)
		rr := newMockErrorResponseWriter()
		req := httptest.NewRequest("POST", "/api/gitrepos/search", strings.NewReader(`{"query":"main function"}`))
		req = setUser(req, "test-user")

		handler.HandleSearchAll(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), "main.go")
	})

	t.Run("DownstreamFailure", func(t *testing.T) {
		// Use an unreachable URL to simulate downstream failure
		handler := newGitReposHandler("http://127.0.0.1:1")
		rr := newMockErrorResponseWriter()
		req := httptest.NewRequest("GET", "/api/gitrepos", nil)
		req = setUser(req, "test-user")

		handler.HandleListRepos(rr, req)

		assert.Equal(t, http.StatusBadGateway, rr.Code)
	})

	t.Run("Downstream5xx", func(t *testing.T) {
		downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"internal error"}`))
		}))
		defer downstream.Close()

		handler := newGitReposHandler(downstream.URL)
		rr := newMockErrorResponseWriter()
		req := httptest.NewRequest("GET", "/api/gitrepos", nil)
		req = setUser(req, "test-user")

		handler.HandleListRepos(rr, req)

		// Proxy passes through downstream status codes
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
	})

	t.Run("Downstream404", func(t *testing.T) {
		downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"repo not found"}`))
		}))
		defer downstream.Close()

		handler := newGitReposHandler(downstream.URL)
		rr := newMockErrorResponseWriter()
		req := httptest.NewRequest("GET", "/api/gitrepos/nonexistent", nil)
		req = mux.SetURLVars(req, map[string]string{"name": "nonexistent"})
		req = setUser(req, "test-user")

		handler.HandleGetRepo(rr, req)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})

	t.Run("MissingPathParam", func(t *testing.T) {
		downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("downstream should not be called")
		}))
		defer downstream.Close()

		handler := newGitReposHandler(downstream.URL)
		rr := newMockErrorResponseWriter()
		// No mux vars set — simulates missing {name}
		req := httptest.NewRequest("GET", "/api/gitrepos/", nil)
		req = setUser(req, "test-user")

		handler.HandleGetRepo(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("TrailingSlashInURL", func(t *testing.T) {
		downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/repos", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[]`))
		}))
		defer downstream.Close()

		// URL with trailing slash should be trimmed
		handler := newGitReposHandler(downstream.URL + "/")
		rr := newMockErrorResponseWriter()
		req := httptest.NewRequest("GET", "/api/gitrepos", nil)
		req = setUser(req, "test-user")

		handler.HandleListRepos(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})
}
