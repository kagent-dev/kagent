package handlers

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
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
// Traffic is proxied directly to the actor ateom pod IP on port 80 (no atenet-router fallback).
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
	target, host, err := substrateGatewayPodTarget(podIP)
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

func substrateGatewayPodTarget(podIP string) (*url.URL, string, error) {
	ip := strings.TrimSpace(podIP)
	if ip == "" || net.ParseIP(ip) == nil {
		return nil, "", fmt.Errorf("invalid actor pod IP %q", podIP)
	}
	target, err := url.Parse("http://" + net.JoinHostPort(ip, "80"))
	if err != nil {
		return nil, "", fmt.Errorf("parse actor pod target: %w", err)
	}
	return target, ip, nil
}

func agentHarnessHarnessBase(namespace, name string) string {
	return "/api/agentharnesses/" + namespace + "/" + name
}

func agentHarnessGatewayPublicPrefix(namespace, name string) string {
	return agentHarnessHarnessBase(namespace, name) + "/gateway/"
}

// resolveGatewayUpstreamPath maps the public URL to the upstream path on the actor.
// redirectTo is set when the browser should use a trailing slash under /gateway/.
// HTTP and WebSocket upgrades to the gateway entry both proxy to upstream / (OpenClaw gateway UI).
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
		_ = wsUpgrade
		return "/", agentHarnessGatewayPublicPrefix(namespace, name), true
	case strings.HasPrefix(rel, "/gateway/"):
		sub := strings.TrimPrefix(rel, "/gateway")
		if sub == "" {
			sub = "/"
		}
		return sub, "", true
	case isHarnessStaticAssetPath(rel):
		return rel, "", true
	default:
		return "", "", false
	}
}

func isHarnessStaticAssetPath(rel string) bool {
	if strings.HasPrefix(rel, "/assets/") {
		return true
	}
	switch rel {
	case "/manifest.webmanifest", "/vite.svg", "/favicon.ico":
		return true
	}
	return strings.HasPrefix(rel, "/favicon")
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
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1
	proxy.Transport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 0,
		IdleConnTimeout:       90 * time.Second,
	}
	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		origDirector(req)
		req.Host = upstreamHost
		req.Header.Set("Host", upstreamHost)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("x-openclaw-scopes", openclawDefaultOperatorScopes)
		normalizeOpenClawBrowserOrigin(req)
		subPath, _, pathOK := resolveGatewayUpstreamPath(req.URL.Path, namespace, name, isWebSocketUpgrade(req))
		if !pathOK {
			subPath = "/"
		}
		if subPath == "" {
			subPath = "/"
		} else if !strings.HasPrefix(subPath, "/") {
			subPath = "/" + subPath
		}
		req.URL.Path = subPath
		req.URL.RawPath = subPath
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Do not read or rewrite WebSocket upgrade responses (would break 101 handshakes).
		if resp.StatusCode == http.StatusSwitchingProtocols {
			return nil
		}

		resp.Header.Del("Content-Security-Policy")
		resp.Header.Del("Content-Security-Policy-Report-Only")

		if loc := resp.Header.Get("Location"); loc != "" {
			if strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, publicPrefix) {
				resp.Header.Set("Location", strings.TrimSuffix(publicPrefix, "/")+loc)
			}
		}

		ct := resp.Header.Get("Content-Type")
		if !shouldRewriteGatewayBody(ct) {
			return nil
		}
		body, err := readGatewayResponseBody(resp)
		if err != nil {
			return err
		}
		rewritten := rewriteGatewayBody(body, ct, publicPrefix)
		if strings.Contains(strings.ToLower(ct), "text/html") {
			rewritten = injectGatewayClientShim(rewritten, token)
		}
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
		resp.ContentLength = int64(len(rewritten))
		resp.Body = io.NopCloser(bytes.NewReader(rewritten))
		return nil
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
		log.Error(proxyErr, "gateway proxy error", "host", upstreamHost)
		http.Error(rw, "gateway proxy error", http.StatusBadGateway)
	}
	return proxy
}

func readGatewayResponseBody(resp *http.Response) ([]byte, error) {
	var reader io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}
	defer resp.Body.Close()
	return io.ReadAll(reader)
}

func (h *Handlers) resolveHarnessGatewayToken(ctx context.Context, ah *v1alpha2.AgentHarness) (string, error) {
	return substrate.ResolveGatewayToken(ctx, h.KubeClient, ah)
}
