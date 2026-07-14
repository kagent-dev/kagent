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

// AgentHarnessGatewayConfig configures the Substrate harness /acp WebSocket
// proxy. Traffic is proxied through atenet-router (Envoy) using actor
// Host-based routing.
type AgentHarnessGatewayConfig struct {
	AtenetRouterURL string
}

// HandleAgentHarnessGateway proxies the browser /acp WebSocket to the actor's
// acp-shim via atenet-router. It is the only externally reachable surface for
// a Substrate harness actor: every backend (including OpenClaw) is reached
// purely over ACP, never the in-sandbox gateway directly.
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

	if h.AgentHarnessSessionActor == nil {
		http.Error(w, "substrate session actor backend is not configured", http.StatusServiceUnavailable)
		return
	}

	// The only externally reachable path is the per-session /acp WebSocket; the
	// actor's in-sandbox gateway is never exposed. Each chat session maps to its
	// own substrate actor, selected by the {sessionId} path segment.
	acpPrefix := agentHarnessHarnessBase(namespace, name) + "/acp/"
	if !strings.HasPrefix(r.URL.Path, acpPrefix) {
		http.NotFound(w, r)
		return
	}
	sessionID := strings.TrimPrefix(r.URL.Path, acpPrefix)
	if sessionID == "" || strings.Contains(sessionID, "/") {
		http.Error(w, "a single session id path segment is required", http.StatusBadRequest)
		return
	}

	// Provision (create + resume) the harness's shared actor on demand, then
	// route to it. If the chat was already provisioned via the ensure endpoint
	// this is a fast no-op that just resumes a suspended actor.
	ensureRes, err := h.AgentHarnessSessionActor.EnsureSessionActor(r.Context(), &ah, sessionID)
	if err != nil {
		log.Info("ensure session actor failed", "session", sessionID, "error", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	target, upstreamHost, err := h.resolveSubstrateGatewayTarget(r.Context(), ensureRes.Handle.Atespace, ensureRes.Handle.ID)
	if err != nil {
		log.Info("resolve substrate gateway target failed", "error", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	proxy := newAgentHarnessACPProxy(target, upstreamHost, log)
	proxy.ServeHTTP(w, r)

	// The session actor is intentionally left running after the WebSocket
	// disconnects so multiple chats can stay live concurrently (e.g. when the
	// user switches between chats). Actors are only suspended when the user
	// explicitly suspends a session via the suspend endpoint.
}

func (h *Handlers) resolveSubstrateGatewayTarget(ctx context.Context, atespace, actorID string) (*url.URL, string, error) {
	cfg := h.AgentHarnessGateway
	if cfg == nil {
		return nil, "", fmt.Errorf("substrate gateway is not configured")
	}

	actorID = strings.TrimSpace(actorID)
	target, host, err := substrate.GatewayRouterTarget(cfg.AtenetRouterURL, atespace, actorID)
	if err != nil {
		return nil, "", fmt.Errorf("substrate actor %q: %w", actorID, err)
	}
	ctrllog.FromContext(ctx).WithName("agentharness-gateway").Info(
		"proxying via atenet-router",
		"atespace", atespace,
		"actor", actorID,
		"router", target.String(),
		"host", host,
	)
	return target, host, nil
}

func agentHarnessHarnessBase(namespace, name string) string {
	return "/api/agentharnesses/" + namespace + "/" + name
}

func newAgentHarnessACPProxy(target *url.URL, upstreamHost string, log interface {
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
			pr.Out.URL.Path = "/acp"
			pr.Out.URL.RawPath = "/acp"
		},
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
		log.Error(proxyErr, "acp proxy error", "host", upstreamHost)
		http.Error(rw, "acp proxy error", http.StatusBadGateway)
	}
	return proxy
}
