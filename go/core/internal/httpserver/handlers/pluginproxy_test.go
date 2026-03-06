package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/kagent-dev/kagent/go/api/database"
	fake "github.com/kagent-dev/kagent/go/core/internal/database/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPluginProxyHandlerWithFakeDB(t *testing.T) (*PluginProxyHandler, *fake.InMemoryFakeClient) {
	t.Helper()
	dbClient := fake.NewClient()
	fakeClient, ok := dbClient.(*fake.InMemoryFakeClient)
	require.True(t, ok)
	base := &Base{DatabaseService: dbClient}
	return NewPluginProxyHandler(base), fakeClient
}

func TestPluginProxyHandler_NotFound(t *testing.T) {
	h, _ := newPluginProxyHandlerWithFakeDB(t)

	req := httptest.NewRequest(http.MethodGet, "/plugins/kanban/api/board", nil)
	req = mux.SetURLVars(req, map[string]string{"name": "kanban"})
	w := httptest.NewRecorder()

	h.HandleProxy(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "plugin not found")
}

func TestPluginProxyHandler_StripsPrefixAndForwardsHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"path":             r.URL.Path,
			"forwarded_host":   r.Header.Get("X-Forwarded-Host"),
			"plugin_name":      r.Header.Get("X-Plugin-Name"),
			"request_host_hdr": r.Host,
		})
	}))
	defer upstream.Close()

	h, fakeClient := newPluginProxyHandlerWithFakeDB(t)
	_, err := fakeClient.StorePlugin(&database.Plugin{
		Name:        "kagent/kanban-mcp",
		PathPrefix:  "kanban",
		DisplayName: "Kanban Board",
		Icon:        "kanban",
		Section:     "AGENTS",
		UpstreamURL: upstream.URL,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/plugins/kanban/api/board", nil)
	req.Host = "kagent.dev"
	req = mux.SetURLVars(req, map[string]string{"name": "kanban"})
	w := httptest.NewRecorder()

	h.HandleProxy(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var got map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "/api/board", got["path"])
	assert.Equal(t, "kagent.dev", got["forwarded_host"])
	assert.Equal(t, "kanban", got["plugin_name"])
}

func TestPluginProxyHandler_UsesProxyCache(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	h, fakeClient := newPluginProxyHandlerWithFakeDB(t)
	_, err := fakeClient.StorePlugin(&database.Plugin{
		Name:        "kagent/kanban-mcp",
		PathPrefix:  "kanban",
		DisplayName: "Kanban Board",
		Icon:        "kanban",
		Section:     "AGENTS",
		UpstreamURL: upstream.URL,
	})
	require.NoError(t, err)

	// First request creates cache entry
	req1 := httptest.NewRequest(http.MethodGet, "/plugins/kanban/", nil)
	req1 = mux.SetURLVars(req1, map[string]string{"name": "kanban"})
	w1 := httptest.NewRecorder()
	h.HandleProxy(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code)

	cached1, ok := h.proxies.Load("kanban")
	require.True(t, ok, "expected proxy cache entry after first request")

	// Second request should reuse same cache entry
	req2 := httptest.NewRequest(http.MethodGet, "/plugins/kanban/api/tasks", nil)
	req2 = mux.SetURLVars(req2, map[string]string{"name": "kanban"})
	w2 := httptest.NewRecorder()
	h.HandleProxy(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	cached2, ok := h.proxies.Load("kanban")
	require.True(t, ok, "expected proxy cache entry after second request")
	assert.Same(t, cached1, cached2, "expected cached reverse proxy instance to be reused")
}

func TestPluginProxyHandler_ProxyRootPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	h, fakeClient := newPluginProxyHandlerWithFakeDB(t)
	_, err := fakeClient.StorePlugin(&database.Plugin{
		Name:        "kagent/kanban-mcp",
		PathPrefix:  "kanban",
		DisplayName: "Kanban Board",
		Icon:        "kanban",
		Section:     "AGENTS",
		UpstreamURL: upstream.URL,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/plugins/kanban", nil)
	req = mux.SetURLVars(req, map[string]string{"name": "kanban"})
	w := httptest.NewRecorder()

	h.HandleProxy(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ok", w.Body.String())
}
