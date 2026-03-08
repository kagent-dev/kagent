package handlers

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
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
	proxyPrefix := "/_p/" + plugin.PathPrefix
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Header.Set("X-Forwarded-Host", req.Host)
			req.Header.Set("X-Plugin-Name", plugin.PathPrefix)
			// Remove Accept-Encoding so we get uncompressed responses for rewriting
			req.Header.Del("Accept-Encoding")
		},
		ModifyResponse: makePathRewriter(proxyPrefix),
		// Flush immediately for SSE support
		FlushInterval: -1,
	}

	h.proxies.Store(plugin.PathPrefix, proxy)
	return proxy
}

// cspMetaRe matches <meta http-equiv="content-security-policy" ...> tags.
// We strip these because rewriting inline scripts invalidates their CSP hashes.
var cspMetaRe = regexp.MustCompile(`(?i)<meta[^>]+http-equiv=["']content-security-policy["'][^>]*>`)

// makePathRewriter returns a ModifyResponse function that rewrites absolute
// paths in HTML responses so that SPA assets load through the plugin proxy.
// For example, href="/_app/foo.js" becomes href="/_p/temporal/_app/foo.js".
func makePathRewriter(proxyPrefix string) func(*http.Response) error {
	return func(resp *http.Response) error {
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			return nil
		}

		var body []byte
		var err error

		// Handle gzipped responses
		if resp.Header.Get("Content-Encoding") == "gzip" {
			gr, gzErr := gzip.NewReader(resp.Body)
			if gzErr != nil {
				return nil // can't decompress, pass through
			}
			body, err = io.ReadAll(gr)
			gr.Close()
			resp.Header.Del("Content-Encoding")
		} else {
			body, err = io.ReadAll(resp.Body)
		}
		resp.Body.Close()
		if err != nil {
			return nil
		}

		// Rewrite absolute paths that reference SPA assets.
		// Common patterns: /_app/, /_next/, /assets/, /static/
		// We use a simple approach: rewrite href="/ and src="/ to include the proxy prefix.
		content := string(body)

		// Strip CSP meta tags — rewriting inline scripts invalidates hash-based CSP
		content = cspMetaRe.ReplaceAllString(content, "")

		// Rewrite SvelteKit base path so all dynamic imports/routing work
		content = strings.ReplaceAll(content, `base: ""`, `base: "`+proxyPrefix+`"`)

		// Rewrite absolute asset paths in link/script tags and dynamic imports
		content = strings.ReplaceAll(content, `"/_app/`, `"`+proxyPrefix+`/_app/`)

		rewritten := []byte(content)
		resp.Body = io.NopCloser(bytes.NewReader(rewritten))
		resp.ContentLength = int64(len(rewritten))
		resp.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))

		return nil
	}
}

// InvalidateCache removes a cached proxy (called when plugin is updated/deleted)
func (h *PluginProxyHandler) InvalidateCache(pathPrefix string) {
	h.proxies.Delete(pathPrefix)
}
