package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strconv"
	"strings"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// PluginProxyPrefix is the root path segment under which RemoteMCPServer web UIs
	// are reverse-proxied: /_p/{pathPrefix}/...
	PluginProxyPrefix = "/_p"

	defaultPluginIcon    = "puzzle"
	defaultPluginSection = "RESOURCES"
)

// PluginsHandler surfaces RemoteMCPServers that expose a web UI (spec.ui.enabled) as
// kagent UI plugins: a registry at GET /api/plugins and a reverse proxy at
// /_p/{pathPrefix}/ to the server's web root.
type PluginsHandler struct {
	*Base
}

// NewPluginsHandler creates a new PluginsHandler.
func NewPluginsHandler(base *Base) *PluginsHandler {
	return &PluginsHandler{Base: base}
}

// HandleListPlugins handles GET /api/plugins requests. It lists RemoteMCPServers across
// the watched namespaces (all namespaces when none are configured) whose spec.ui.enabled
// is true, projects each to api.PluginResponse (applying pathPrefix/displayName=name and
// icon/section defaults), and returns them sorted by pathPrefix.
func (h *PluginsHandler) HandleListPlugins(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("plugins-handler").WithValues("operation", "list")
	log.Info("Received request to list plugins")

	if err := Check(h.Authorizer, r, auth.Resource{Type: "ToolServer"}); err != nil {
		w.RespondWithError(err)
		return
	}

	servers, err := h.listEnabledUIServers(r.Context())
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list RemoteMCPServers", err))
		return
	}

	plugins := make([]api.PluginResponse, 0, len(servers))
	for i := range servers {
		plugins = append(plugins, pluginResponseFor(&servers[i]))
	}

	slices.SortFunc(plugins, func(a, b api.PluginResponse) int {
		return strings.Compare(a.PathPrefix, b.PathPrefix)
	})

	log.Info("Successfully listed plugins", "count", len(plugins))
	RespondWithJSON(w, http.StatusOK, api.NewResponse(plugins, "Successfully listed plugins", false))
}

// HandleProxy handles /_p/{pathPrefix}/* requests by reverse-proxying to the web root of
// the RemoteMCPServer whose effective ui.pathPrefix matches {pathPrefix}. It authorizes
// against the backing ToolServer, resolves spec.headersFrom, injects spec.ui.injectCSS into
// text/html responses, and returns 502 when the upstream is unreachable.
func (h *PluginsHandler) HandleProxy(w http.ResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("plugins-handler").WithValues("operation", "proxy")

	pathPrefix, err := GetPathParam(r, "pathPrefix")
	if err != nil {
		http.Error(w, "missing path prefix", http.StatusBadRequest)
		return
	}

	server, err := h.findUIServerByPrefix(r.Context(), pathPrefix)
	if err != nil {
		log.Error(err, "Failed to resolve plugin", "pathPrefix", pathPrefix)
		http.Error(w, "failed to resolve plugin", http.StatusInternalServerError)
		return
	}
	if server == nil {
		http.Error(w, fmt.Sprintf("no plugin registered for %q", pathPrefix), http.StatusNotFound)
		return
	}

	if apiErr := Check(h.Authorizer, r, auth.Resource{
		Type: "ToolServer",
		Name: types.NamespacedName{Namespace: server.Namespace, Name: server.Name}.String(),
	}); apiErr != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	target, err := url.Parse(server.Spec.URL)
	if err != nil {
		log.Error(err, "Invalid RemoteMCPServer URL", "url", server.Spec.URL)
		http.Error(w, "invalid upstream URL", http.StatusInternalServerError)
		return
	}

	headers, err := server.ResolveHeaders(r.Context(), h.KubeClient)
	if err != nil {
		log.Error(err, "Failed to resolve RemoteMCPServer headers")
		http.Error(w, "failed to resolve upstream headers", http.StatusInternalServerError)
		return
	}

	// Strip /_p/{pathPrefix} so the remainder is proxied to the server's web root ("/").
	// pathPrefix is constrained to [a-z0-9-], so the escaped and unescaped prefixes are
	// identical and we can trim both the decoded and raw paths with the same literal.
	stripPrefix := PluginProxyPrefix + "/" + pathPrefix
	remainder := strings.TrimPrefix(r.URL.Path, stripPrefix)
	remainderRaw := strings.TrimPrefix(r.URL.EscapedPath(), stripPrefix)
	if remainder == "" {
		remainder = "/"
	}
	if remainderRaw == "" {
		remainderRaw = "/"
	}

	injectCSS := ""
	if server.Spec.UI != nil {
		injectCSS = server.Spec.UI.InjectCSS
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			// Keep Path and RawPath in sync so encoded segments survive proxying.
			req.URL.Path = remainder
			req.URL.RawPath = remainderRaw
			// Request an uncompressed response so injectCSS can be spliced reliably.
			req.Header.Set("Accept-Encoding", "identity")
			for k, v := range headers {
				req.Header.Set(k, v)
			}
		},
		ModifyResponse: injectCSSModifier(injectCSS),
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, perr error) {
			log.Error(perr, "Plugin upstream unreachable", "pathPrefix", pathPrefix, "url", server.Spec.URL)
			http.Error(rw, "plugin upstream unreachable", http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(w, r)
}

// injectCSSModifier returns a ReverseProxy ModifyResponse that splices css into text/html
// responses (before </head>, or prepended when absent). A nil/empty css yields a no-op.
func injectCSSModifier(css string) func(*http.Response) error {
	if css == "" {
		return nil
	}
	styleTag := "<style>" + css + "</style>"
	return func(resp *http.Response) error {
		if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
			return nil
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()

		html := string(body)
		if idx := strings.Index(strings.ToLower(html), "</head>"); idx != -1 {
			html = html[:idx] + styleTag + html[idx:]
		} else {
			html = styleTag + html
		}

		buf := []byte(html)
		resp.Body = io.NopCloser(bytes.NewReader(buf))
		resp.ContentLength = int64(len(buf))
		resp.Header.Set("Content-Length", strconv.Itoa(len(buf)))
		return nil
	}
}

// listEnabledUIServers returns all RemoteMCPServers with spec.ui.enabled across the watched
// namespaces, listing cluster-wide when no namespaces are configured.
func (h *PluginsHandler) listEnabledUIServers(ctx context.Context) ([]v1alpha2.RemoteMCPServer, error) {
	var servers []v1alpha2.RemoteMCPServer

	appendEnabled := func(items []v1alpha2.RemoteMCPServer) {
		for i := range items {
			if uiEnabled(&items[i]) {
				servers = append(servers, items[i])
			}
		}
	}

	if len(h.WatchedNamespaces) == 0 {
		list := &v1alpha2.RemoteMCPServerList{}
		if err := h.KubeClient.List(ctx, list); err != nil {
			return nil, fmt.Errorf("failed to list RemoteMCPServers: %w", err)
		}
		appendEnabled(list.Items)
		return servers, nil
	}

	for _, ns := range h.WatchedNamespaces {
		list := &v1alpha2.RemoteMCPServerList{}
		if err := h.KubeClient.List(ctx, list, client.InNamespace(ns)); err != nil {
			return nil, fmt.Errorf("failed to list RemoteMCPServers in namespace %s: %w", ns, err)
		}
		appendEnabled(list.Items)
	}
	return servers, nil
}

// findUIServerByPrefix resolves an enabled UI server by its effective pathPrefix. First
// match wins; returns (nil, nil) when no server matches.
func (h *PluginsHandler) findUIServerByPrefix(ctx context.Context, pathPrefix string) (*v1alpha2.RemoteMCPServer, error) {
	servers, err := h.listEnabledUIServers(ctx)
	if err != nil {
		return nil, err
	}
	for i := range servers {
		if effectivePathPrefix(&servers[i]) == pathPrefix {
			return &servers[i], nil
		}
	}
	return nil, nil
}

// uiEnabled reports whether the server opts into UI plugin registration.
func uiEnabled(s *v1alpha2.RemoteMCPServer) bool {
	return s.Spec.UI != nil && s.Spec.UI.Enabled
}

// effectivePathPrefix returns spec.ui.pathPrefix, falling back to the resource name.
func effectivePathPrefix(s *v1alpha2.RemoteMCPServer) string {
	if s.Spec.UI != nil && s.Spec.UI.PathPrefix != "" {
		return s.Spec.UI.PathPrefix
	}
	return s.Name
}

// pluginResponseFor projects a RemoteMCPServer to an api.PluginResponse, applying the
// pathPrefix/displayName=name and icon=puzzle / section=RESOURCES defaults.
func pluginResponseFor(s *v1alpha2.RemoteMCPServer) api.PluginResponse {
	ui := s.Spec.UI
	resp := api.PluginResponse{
		Name:        s.Name,
		Namespace:   s.Namespace,
		PathPrefix:  effectivePathPrefix(s),
		DisplayName: s.Name,
		Icon:        defaultPluginIcon,
		Section:     defaultPluginSection,
	}
	if ui != nil {
		if ui.DisplayName != "" {
			resp.DisplayName = ui.DisplayName
		}
		if ui.Icon != "" {
			resp.Icon = ui.Icon
		}
		if ui.Section != "" {
			resp.Section = ui.Section
		}
		resp.DefaultPath = ui.DefaultPath
	}
	return resp
}
