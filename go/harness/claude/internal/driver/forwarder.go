package driver

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

const (
	forwarderPathPrefix       = "/mcp/"
	callerCredentialParameter = "authorization"
)

// UpstreamMCPServer is one compiled MCP server the forwarder fronts: its real
// URL and the static headers the compiler assigned to it.
type UpstreamMCPServer struct {
	URL     string
	Headers map[string]string
}

// CallerCredentialBinder receives the caller's credential for the duration of
// one turn. A binder holds nothing between Clear and the next Bind.
type CallerCredentialBinder interface {
	Bind(credential string)
	Clear()
}

// CredentialForwarder fronts the compiled MCP servers on loopback and adds the
// caller's credential of the current turn to every request it forwards. Claude
// receives the loopback address and a per-process token only; the caller's
// credential never enters its environment, its configuration files or the
// Actor's filesystem, and it is held in this process for one turn at a time.
type CredentialForwarder struct {
	token    string
	maxBody  int64
	listener net.Listener
	server   *http.Server
	targets  map[string]*forwardTarget

	mu         sync.Mutex
	credential string
}

type forwardTarget struct {
	url     *url.URL
	headers map[string]string
	proxy   *httputil.ReverseProxy
}

// NewCredentialForwarder binds an authenticated loopback endpoint per server.
// Only streamable HTTP servers are supported: an SSE server announces its
// message endpoint from the upstream host, which the forwarder does not
// rewrite.
func NewCredentialForwarder(servers map[string]UpstreamMCPServer, maxBodyBytes int) (*CredentialForwarder, error) {
	if len(servers) == 0 || maxBodyBytes <= 0 {
		return nil, fmt.Errorf("MCP servers and a positive body limit are required")
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate credential forwarder token: %w", err)
	}
	forwarder := &CredentialForwarder{
		token: hex.EncodeToString(tokenBytes), maxBody: int64(maxBodyBytes),
		targets: make(map[string]*forwardTarget, len(servers)),
	}
	for name, server := range servers {
		if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "/?#") {
			return nil, fmt.Errorf("MCP server name %q cannot be forwarded", name)
		}
		target, err := url.Parse(strings.TrimSpace(server.URL))
		if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
			return nil, fmt.Errorf("MCP server %q URL %q must be an absolute http(s) URL", name, server.URL)
		}
		headers := make(map[string]string, len(server.Headers))
		for header, value := range server.Headers {
			headers[http.CanonicalHeaderKey(header)] = value
		}
		entry := &forwardTarget{url: target, headers: headers}
		entry.proxy = &httputil.ReverseProxy{
			Rewrite:       forwarder.rewrite(entry),
			FlushInterval: -1,
		}
		forwarder.targets[name] = entry
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for Claude MCP calls: %w", err)
	}
	forwarder.listener = listener
	forwarder.server = &http.Server{
		Handler:           forwarder.authorizeAndLimit(http.HandlerFunc(forwarder.serve)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = forwarder.server.Serve(listener) }()
	return forwarder, nil
}

// URL is the loopback endpoint Claude uses for the named server.
func (f *CredentialForwarder) URL(name string) string {
	return "http://" + f.listener.Addr().String() + forwarderPathPrefix + name
}

// Headers returns the credentials for the loopback endpoint.
func (f *CredentialForwarder) Headers() map[string]string {
	return map[string]string{"Authorization": "Bearer " + f.token}
}

// Names lists the fronted servers in a stable order.
func (f *CredentialForwarder) Names() []string {
	names := make([]string, 0, len(f.targets))
	for name := range f.targets {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Bind sets the caller's credential every forwarded request carries until Clear.
func (f *CredentialForwarder) Bind(credential string) {
	f.mu.Lock()
	f.credential = credential
	f.mu.Unlock()
}

// Clear drops the caller's credential; requests are forwarded without one.
func (f *CredentialForwarder) Clear() {
	f.mu.Lock()
	f.credential = ""
	f.mu.Unlock()
}

// Close stops the loopback listener.
func (f *CredentialForwarder) Close() error { return f.server.Close() }

func (f *CredentialForwarder) current() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.credential
}

func (f *CredentialForwarder) authorizeAndLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		want, got := "Bearer "+f.token, request.Header.Get("Authorization")
		if subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		request.Body = http.MaxBytesReader(response, request.Body, f.maxBody)
		next.ServeHTTP(response, request)
	})
}

func (f *CredentialForwarder) serve(response http.ResponseWriter, request *http.Request) {
	rest, ok := strings.CutPrefix(request.URL.Path, forwarderPathPrefix)
	if !ok {
		http.NotFound(response, request)
		return
	}
	name, suffix, _ := strings.Cut(rest, "/")
	target, ok := f.targets[name]
	if !ok {
		http.NotFound(response, request)
		return
	}
	request = request.WithContext(context.WithValue(request.Context(), pathSuffixKey{}, suffix))
	target.proxy.ServeHTTP(response, request)
}

type pathSuffixKey struct{}

func (f *CredentialForwarder) rewrite(target *forwardTarget) func(*httputil.ProxyRequest) {
	return func(proxied *httputil.ProxyRequest) {
		out := proxied.Out
		out.URL.Scheme = target.url.Scheme
		out.URL.Host = target.url.Host
		out.Host = target.url.Host
		suffix, _ := proxied.In.Context().Value(pathSuffixKey{}).(string)
		out.URL.Path, out.URL.RawPath = joinPath(target.url, suffix)
		out.URL.RawQuery = joinQuery(target.url.RawQuery, proxied.In.URL.RawQuery)
		// The loopback token authenticates Claude to this process only.
		out.Header.Del("Authorization")
		for header, value := range target.headers {
			out.Header.Set(header, value)
		}
		if credential := f.current(); credential != "" {
			out.Header.Set("Authorization", credential)
		}
	}
}

func joinPath(target *url.URL, suffix string) (path, rawPath string) {
	if suffix == "" {
		return target.Path, target.RawPath
	}
	joined := target.JoinPath(suffix)
	return joined.Path, joined.RawPath
}

func joinQuery(base, extra string) string {
	switch {
	case base == "":
		return extra
	case extra == "":
		return base
	default:
		return base + "&" + extra
	}
}

// CallerCredential returns the caller's credential of the A2A call that owns
// ctx, or the empty string when the call carried none.
func CallerCredential(ctx context.Context) string {
	callCtx, ok := a2asrv.CallContextFrom(ctx)
	if !ok || callCtx == nil {
		return ""
	}
	params := callCtx.ServiceParams()
	if params == nil {
		return ""
	}
	values, ok := params.Get(callerCredentialParameter)
	if !ok || len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}
