package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// OpenClaw 2026.3.28+ returns 403 without operator scopes on HTTP/WS when only Bearer token is sent.
	openclawDefaultOperatorScopes = "operator.admin"
	// Origin OpenClaw accepts by default for bind=lan port=80 (localhost/127.0.0.1 on gateway port).
	openclawLoopbackOrigin = "http://127.0.0.1:80"
)

// AgentHarnessGatewayConfig configures Substrate harness HTTP/WebSocket proxy.
// Traffic is proxied through atenet-router (Envoy) using actor Host-based routing.
type AgentHarnessGatewayConfig struct {
	AtenetRouterURL string
}

// HandleAgentHarnessGateway proxies browser traffic to the actor OpenClaw gateway via atenet-router.
func (h *Handlers) HandleAgentHarnessGateway(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agentharness-gateway")
	if h.AgentHarnessGateway == nil {
		http.Error(w, "substrate gateway proxy is not configured", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	namespace := strings.TrimSpace(vars["namespace"])
	name := strings.TrimSpace(vars["name"])
	if namespace == "" || name == "" {
		http.Error(w, "namespace and name are required", http.StatusBadRequest)
		return
	}

	var ah v1alpha2.AgentHarness
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{Namespace: namespace, Name: name}, &ah); err != nil {
		if apierrors.IsNotFound(err) {
			http.Error(w, "AgentHarness not found", http.StatusNotFound)
			return
		}
		log.Error(err, "get AgentHarness")
		http.Error(w, "failed to load AgentHarness", http.StatusInternalServerError)
		return
	}

	runtime := ah.Spec.Runtime
	if runtime == "" {
		runtime = v1alpha2.AgentHarnessRuntimeOpenshell
	}
	if runtime != v1alpha2.AgentHarnessRuntimeSubstrate {
		http.Error(w, "gateway proxy is only available for runtime=substrate", http.StatusBadRequest)
		return
	}
	if ah.Status.BackendRef == nil || ah.Status.BackendRef.ID == "" {
		http.Error(w, "harness has no substrate actor yet", http.StatusServiceUnavailable)
		return
	}

	token, err := h.resolveHarnessGatewayToken(r.Context(), &ah)
	if err != nil {
		log.Error(err, "resolve gateway token")
		http.Error(w, "gateway token not configured", http.StatusInternalServerError)
		return
	}

	target, upstreamHost, err := h.resolveSubstrateGatewayTarget(r.Context(), &ah)
	if err != nil {
		log.Info("resolve substrate gateway target failed", "error", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	publicPrefix := agentHarnessGatewayPublicPrefix(namespace, name)

	_, redirectTo, ok := resolveGatewayUpstreamPath(r.URL.Path, namespace, name, isWebSocketUpgrade(r))
	if !ok {
		http.NotFound(w, r)
		return
	}
	// Browsers do not complete WebSocket handshakes through 30x redirects.
	if redirectTo != "" && !isWebSocketUpgrade(r) {
		dest := redirectTo
		if r.URL.RawQuery != "" {
			dest += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, dest, http.StatusPermanentRedirect)
		return
	}

	proxy := newAgentHarnessGatewayProxy(target, upstreamHost, token, publicPrefix, namespace, name, log)
	proxy.ServeHTTP(w, r)
}

func (h *Handlers) resolveSubstrateGatewayTarget(ctx context.Context, ah *v1alpha2.AgentHarness) (*url.URL, string, error) {
	cfg := h.AgentHarnessGateway
	if cfg == nil {
		return nil, "", fmt.Errorf("substrate gateway is not configured")
	}

	actorID := strings.TrimSpace(ah.Status.BackendRef.ID)
	target, host, err := substrate.GatewayRouterTarget(cfg.AtenetRouterURL, actorID)
	if err != nil {
		return nil, "", fmt.Errorf("substrate actor %q: %w", actorID, err)
	}
	ctrllog.FromContext(ctx).WithName("agentharness-gateway").Info(
		"proxying via atenet-router",
		"actor", actorID,
		"router", target.String(),
		"host", host,
	)
	return target, host, nil
}

func agentHarnessHarnessBase(namespace, name string) string {
	return "/api/agentharnesses/" + namespace + "/" + name
}

func agentHarnessGatewayPublicPrefix(namespace, name string) string {
	return agentHarnessHarnessBase(namespace, name) + "/gateway/"
}

// resolveGatewayUpstreamPath maps the public URL to the upstream path on the actor.
// redirectTo is set when the browser should use a trailing slash under /gateway/.
// OpenClaw is configured with the same controlUi.basePath, so the proxy preserves
// the public gateway base path when forwarding to the actor.
func resolveGatewayUpstreamPath(requestPath, namespace, name string, wsUpgrade bool) (upstreamPath, redirectTo string, ok bool) {
	base := agentHarnessHarnessBase(namespace, name)
	if !strings.HasPrefix(requestPath, base) {
		return "", "", false
	}
	rel := strings.TrimPrefix(requestPath, base)
	if rel == "" {
		return "", agentHarnessGatewayPublicPrefix(namespace, name), true
	}

	switch {
	case rel == "/gateway":
		upstream := agentHarnessGatewayPublicPrefix(namespace, name)
		if wsUpgrade {
			return upstream, "", true
		}
		return upstream, upstream, true
	case strings.HasPrefix(rel, "/gateway/"):
		return requestPath, "", true
	default:
		return "", "", false
	}
}

// normalizeOpenClawBrowserOrigin rewrites Origin/Referer so OpenClaw accepts WS/API from kagent-ui
// (e.g. http://localhost:8001) while the gateway listens on the actor pod :80.
func normalizeOpenClawBrowserOrigin(req *http.Request) {
	if req == nil {
		return
	}
	if req.Header.Get("Origin") != "" {
		req.Header.Set("Origin", openclawLoopbackOrigin)
	}
	if req.Header.Get("Referer") != "" {
		req.Header.Set("Referer", openclawLoopbackOrigin+"/")
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	if r == nil {
		return false
	}
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

func newAgentHarnessGatewayProxy(target *url.URL, upstreamHost, token, publicPrefix, namespace, name string, log interface {
	Error(error, string, ...any)
}) *httputil.ReverseProxy {
	proxy := &httputil.ReverseProxy{
		FlushInterval: -1,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 0,
			IdleConnTimeout:       90 * time.Second,
		},
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = upstreamHost
			if token != "" {
				pr.Out.Header.Set("Authorization", "Bearer "+token)
			}
			pr.Out.Header.Set("x-openclaw-scopes", openclawDefaultOperatorScopes)
			normalizeOpenClawBrowserOrigin(pr.Out)
			subPath, _, pathOK := resolveGatewayUpstreamPath(pr.In.URL.Path, namespace, name, isWebSocketUpgrade(pr.In))
			if !pathOK {
				subPath = "/"
			}
			if subPath == "" {
				subPath = "/"
			} else if !strings.HasPrefix(subPath, "/") {
				subPath = "/" + subPath
			}
			pr.Out.URL.Path = subPath
			pr.Out.URL.RawPath = subPath
		},
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode == http.StatusSwitchingProtocols {
			return nil
		}

		if loc := resp.Header.Get("Location"); loc != "" {
			publicBase := strings.TrimSuffix(publicPrefix, "/")
			if strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, publicBase) {
				resp.Header.Set("Location", publicBase+loc)
			}
		}
		return nil
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
		log.Error(proxyErr, "gateway proxy error", "host", upstreamHost)
		http.Error(rw, "gateway proxy error", http.StatusBadGateway)
	}
	return proxy
}

func (h *Handlers) resolveHarnessGatewayToken(ctx context.Context, ah *v1alpha2.AgentHarness) (string, error) {
	return substrate.ResolveGatewayToken(ctx, h.KubeClient, ah)
}
