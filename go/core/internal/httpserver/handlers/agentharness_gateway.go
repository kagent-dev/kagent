package handlers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// OpenClaw 2026.3.28+ returns 403 without operator scopes on HTTP/WS when only Bearer token is sent.
	openclawDefaultOperatorScopes = "operator.admin"
)

func openclawLoopbackOrigin(port int32) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// AgentHarnessGatewayConfig configures Substrate harness HTTP/WebSocket proxy.
// Traffic is proxied directly to the actor ateom pod IP on spec.substrate.gatewayPort (default 80).
type AgentHarnessGatewayConfig struct {
	AteAPIEndpoint string
	AteAPIInsecure bool
	DialTimeout    time.Duration
	CallTimeout    time.Duration
}

// HandleAgentHarnessGateway proxies browser traffic to the actor OpenClaw gateway (pod IP when available).
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

	if h.Agents == nil {
		http.Error(w, "agents handler is not configured", http.StatusInternalServerError)
		return
	}
	agentRef := types.NamespacedName{Namespace: namespace, Name: name}.String()
	if err := Check(h.Agents.Authorizer, r, auth.Resource{Type: "Agent", Name: agentRef}); err != nil {
		w.RespondWithError(err)
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

	publicPrefix := substrate.AgentHarnessGatewayUIPath(namespace, name)

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

	gwPort := substrate.GatewayPort(&ah)
	proxy := newAgentHarnessGatewayProxy(target, upstreamHost, token, publicPrefix, namespace, name, gwPort, log)
	proxy.ServeHTTP(w, r)
}

func (h *Handlers) resolveSubstrateGatewayTarget(ctx context.Context, ah *v1alpha2.AgentHarness) (*url.URL, string, error) {
	cfg := h.AgentHarnessGateway
	if cfg == nil {
		return nil, "", fmt.Errorf("substrate gateway is not configured")
	}
	if cfg.AteAPIEndpoint == "" {
		return nil, "", fmt.Errorf("substrate ate-api is not configured on the controller")
	}

	ateClient, err := substrate.Dial(ctx, substrate.Config{
		AteAPIEndpoint: cfg.AteAPIEndpoint,
		Insecure:       cfg.AteAPIInsecure,
		DialTimeout:    cfg.DialTimeout,
		CallTimeout:    cfg.CallTimeout,
	})
	if err != nil {
		return nil, "", fmt.Errorf("dial ate-api: %w", err)
	}
	defer ateClient.Close()

	actorID := ah.Status.BackendRef.ID
	actor, err := ateClient.GetActor(ctx, actorID)
	if err != nil {
		return nil, "", fmt.Errorf("get substrate actor %q: %w", actorID, err)
	}
	podIP := strings.TrimSpace(actor.GetAteomPodIp())
	if podIP == "" {
		return nil, "", fmt.Errorf("substrate actor %q has no pod IP (status %s; resume the actor and wait until running)", actorID, actor.GetStatus())
	}
	port := substrate.GatewayPort(ah)
	target, host, err := substrateGatewayPodTarget(podIP, port)
	if err != nil {
		return nil, "", fmt.Errorf("substrate actor %q pod IP %q: %w", actorID, podIP, err)
	}
	ctrllog.FromContext(ctx).WithName("agentharness-gateway").Info(
		"proxying via actor pod IP",
		"actor", actorID,
		"podIP", host,
	)
	return target, host, nil
}

func substrateGatewayPodTarget(podIP string, port int32) (*url.URL, string, error) {
	ip := strings.TrimSpace(podIP)
	if ip == "" || net.ParseIP(ip) == nil {
		return nil, "", fmt.Errorf("invalid actor pod IP %q", podIP)
	}
	if port <= 0 {
		port = 80
	}
	target, err := url.Parse("http://" + net.JoinHostPort(ip, fmt.Sprintf("%d", port)))
	if err != nil {
		return nil, "", fmt.Errorf("parse actor pod target: %w", err)
	}
	return target, ip, nil
}

func agentHarnessHarnessBase(namespace, name string) string {
	return substrate.AgentHarnessAPIBase(namespace, name)
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
		return "", substrate.AgentHarnessGatewayUIPath(namespace, name), true
	}

	switch {
	case rel == "/gateway":
		upstream := substrate.AgentHarnessGatewayUIPath(namespace, name)
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
// (e.g. http://localhost:8001) while the gateway listens on the actor pod.
func normalizeOpenClawBrowserOrigin(req *http.Request, gwPort int32) {
	if req == nil {
		return
	}
	origin := openclawLoopbackOrigin(gwPort)
	if req.Header.Get("Origin") != "" {
		req.Header.Set("Origin", origin)
	}
	if req.Header.Get("Referer") != "" {
		req.Header.Set("Referer", origin+"/")
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	if r == nil {
		return false
	}
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

func newAgentHarnessGatewayProxy(target *url.URL, upstreamHost, token, publicPrefix, namespace, name string, gwPort int32, log interface {
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
			normalizeOpenClawBrowserOrigin(pr.Out, gwPort)
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
