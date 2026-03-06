package handlers

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	"github.com/kagent-dev/kagent/go/api/database"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// PluginProxyHandler handles /_p/{name}/ reverse proxy requests
type PluginProxyHandler struct {
	*Base
	proxies sync.Map // pathPrefix -> *httputil.ReverseProxy
}

// NewPluginProxyHandler creates a new PluginProxyHandler
func NewPluginProxyHandler(base *Base) *PluginProxyHandler {
	return &PluginProxyHandler{Base: base}
}

// HandleProxy handles all requests to /_p/{name}/{path...}
func (h *PluginProxyHandler) HandleProxy(w http.ResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("plugin-proxy")

	pathPrefix := mux.Vars(r)["name"]
	if pathPrefix == "" {
		http.Error(w, "plugin name required", http.StatusBadRequest)
		return
	}

	plugin, err := h.DatabaseService.GetPluginByPathPrefix(pathPrefix)
	if err != nil {
		log.V(1).Info("Plugin not found", "pathPrefix", pathPrefix)
		http.Error(w, "plugin not found", http.StatusNotFound)
		return
	}

	proxy := h.getOrCreateProxy(plugin)

	// Strip the /_p/{name} prefix before forwarding
	originalPath := r.URL.Path
	prefix := "/_p/" + pathPrefix
	r.URL.Path = strings.TrimPrefix(originalPath, prefix)
	if r.URL.Path == "" {
		r.URL.Path = "/"
	}

	proxy.ServeHTTP(w, r)
}

func (h *PluginProxyHandler) getOrCreateProxy(plugin *database.Plugin) *httputil.ReverseProxy {
	if cached, ok := h.proxies.Load(plugin.PathPrefix); ok {
		return cached.(*httputil.ReverseProxy)
	}

	target, _ := url.Parse(plugin.UpstreamURL)
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Header.Set("X-Forwarded-Host", req.Host)
			req.Header.Set("X-Plugin-Name", plugin.PathPrefix)
		},
		// Flush immediately for SSE support
		FlushInterval: -1,
	}

	h.proxies.Store(plugin.PathPrefix, proxy)
	return proxy
}

// InvalidateCache removes a cached proxy (called when plugin is updated/deleted)
func (h *PluginProxyHandler) InvalidateCache(pathPrefix string) {
	h.proxies.Delete(pathPrefix)
}
