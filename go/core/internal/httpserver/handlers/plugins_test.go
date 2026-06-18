package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
)

func newUIServer(name, namespace string, ui *v1alpha2.RemoteMCPServerUI, url string) *v1alpha2.RemoteMCPServer {
	return &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description: name,
			URL:         url,
			UI:          ui,
		},
	}
}

func setupPluginsHandler(t *testing.T, objs ...*v1alpha2.RemoteMCPServer) *handlers.PluginsHandler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, o := range objs {
		builder = builder.WithObjects(o)
	}
	base := &handlers.Base{
		KubeClient: builder.Build(),
		Authorizer: &authimpl.NoopAuthorizer{},
	}
	return handlers.NewPluginsHandler(base)
}

func TestHandleListPlugins(t *testing.T) {
	t.Run("FiltersDefaultsAndSorts", func(t *testing.T) {
		handler := setupPluginsHandler(t,
			// enabled, fully specified
			newUIServer("zeta", "default", &v1alpha2.RemoteMCPServerUI{
				Enabled:     true,
				PathPrefix:  "zeta-board",
				DisplayName: "Zeta Board",
				Icon:        "kanban",
				Section:     "RESOURCES",
				DefaultPath: "/home",
			}, "http://zeta.default:8080/mcp"),
			// enabled, relies on defaults (pathPrefix/displayName=name, icon=puzzle, section=RESOURCES)
			newUIServer("alpha", "default", &v1alpha2.RemoteMCPServerUI{Enabled: true}, "http://alpha.default:8080/mcp"),
			// ui present but disabled -> excluded
			newUIServer("disabled", "default", &v1alpha2.RemoteMCPServerUI{Enabled: false}, "http://disabled.default:8080/mcp"),
			// no ui block -> excluded
			newUIServer("noui", "default", nil, "http://noui.default:8080/mcp"),
		)

		req := httptest.NewRequest(http.MethodGet, "/api/plugins", nil)
		req = setUser(req, "test-user")
		rec := newMockErrorResponseWriter()

		handler.HandleListPlugins(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var resp api.StandardResponse[[]api.PluginResponse]
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Len(t, resp.Data, 2)

		// Sorted by pathPrefix: "alpha" < "zeta-board".
		alpha := resp.Data[0]
		assert.Equal(t, "alpha", alpha.Name)
		assert.Equal(t, "alpha", alpha.PathPrefix)
		assert.Equal(t, "alpha", alpha.DisplayName)
		assert.Equal(t, "puzzle", alpha.Icon)
		assert.Equal(t, "RESOURCES", alpha.Section)

		zeta := resp.Data[1]
		assert.Equal(t, "zeta", zeta.Name)
		assert.Equal(t, "zeta-board", zeta.PathPrefix)
		assert.Equal(t, "Zeta Board", zeta.DisplayName)
		assert.Equal(t, "kanban", zeta.Icon)
		assert.Equal(t, "RESOURCES", zeta.Section)
		assert.Equal(t, "/home", zeta.DefaultPath)
	})

	t.Run("EmptyWhenNoneEnabled", func(t *testing.T) {
		handler := setupPluginsHandler(t,
			newUIServer("noui", "default", nil, "http://noui.default:8080/mcp"),
		)

		req := httptest.NewRequest(http.MethodGet, "/api/plugins", nil)
		req = setUser(req, "test-user")
		rec := newMockErrorResponseWriter()

		handler.HandleListPlugins(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp api.StandardResponse[[]api.PluginResponse]
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Len(t, resp.Data, 0)
	})
}

func TestHandleProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><head><title>Board</title></head><body>hi</body></html>"))
		case "/api/board":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	newProxyRouter := func(handler *handlers.PluginsHandler) *mux.Router {
		router := mux.NewRouter()
		router.PathPrefix(handlers.PluginProxyPrefix + "/{pathPrefix}").Handler(http.HandlerFunc(handler.HandleProxy))
		return router
	}

	serve := func(handler *handlers.PluginsHandler, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = setUser(req, "test-user")
		rec := httptest.NewRecorder()
		newProxyRouter(handler).ServeHTTP(rec, req)
		return rec
	}

	t.Run("ProxiesRootWithCSSInjection", func(t *testing.T) {
		handler := setupPluginsHandler(t,
			newUIServer("kanban", "default", &v1alpha2.RemoteMCPServerUI{
				Enabled:    true,
				PathPrefix: "kanban",
				InjectCSS:  "body{color:red}",
			}, upstream.URL+"/mcp"),
		)

		rec := serve(handler, "/_p/kanban/")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		body := rec.Body.String()
		assert.Contains(t, body, "<title>Board</title>")
		assert.Contains(t, body, "<style>body{color:red}</style></head>")
	})

	t.Run("ProxiesSubpath", func(t *testing.T) {
		handler := setupPluginsHandler(t,
			newUIServer("kanban", "default", &v1alpha2.RemoteMCPServerUI{
				Enabled:    true,
				PathPrefix: "kanban",
			}, upstream.URL+"/mcp"),
		)

		rec := serve(handler, "/_p/kanban/api/board")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.JSONEq(t, `{"ok":true}`, rec.Body.String())
	})

	t.Run("UnknownPrefixReturns404", func(t *testing.T) {
		handler := setupPluginsHandler(t,
			newUIServer("kanban", "default", &v1alpha2.RemoteMCPServerUI{
				Enabled:    true,
				PathPrefix: "kanban",
			}, upstream.URL+"/mcp"),
		)

		rec := serve(handler, "/_p/does-not-exist/")
		require.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("DisabledServerNotProxied", func(t *testing.T) {
		handler := setupPluginsHandler(t,
			newUIServer("kanban", "default", &v1alpha2.RemoteMCPServerUI{
				Enabled:    false,
				PathPrefix: "kanban",
			}, upstream.URL+"/mcp"),
		)

		rec := serve(handler, "/_p/kanban/")
		require.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("UnreachableUpstreamReturns502", func(t *testing.T) {
		handler := setupPluginsHandler(t,
			newUIServer("kanban", "default", &v1alpha2.RemoteMCPServerUI{
				Enabled:    true,
				PathPrefix: "kanban",
			}, "http://127.0.0.1:1/mcp"),
		)

		rec := serve(handler, "/_p/kanban/")
		require.Equal(t, http.StatusBadGateway, rec.Code)
	})

	t.Run("EffectivePrefixDefaultsToName", func(t *testing.T) {
		// No pathPrefix set -> effective prefix is the resource name.
		handler := setupPluginsHandler(t,
			newUIServer("kanban", "default", &v1alpha2.RemoteMCPServerUI{Enabled: true}, upstream.URL+"/mcp"),
		)

		rec := serve(handler, "/_p/kanban/api/board")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.JSONEq(t, `{"ok":true}`, rec.Body.String())
	})
}
