// Package forwarder fronts the upstreams a native harness process calls with
// the caller's credential on an authenticated loopback endpoint. The native
// process receives loopback URLs and a per-process token only; the caller's
// credential stays in the harness process and is held for one turn at a time.
package forwarder

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
)

const (
	mcpPathPrefix = "/mcp/"
	modelPath     = "/model"
)

// Routes are the upstreams a forwarder fronts.
type Routes struct {
	MCPServers map[string]MCPServer
	// Model is set when the model receives the caller's credential as its API
	// key (ModelConfig tokenPassthrough).
	Model *Model
}

// MCPServer is one compiled MCP server the forwarder fronts: its real URL and
// the static headers the compiler assigned to it, with their values resolved.
type MCPServer struct {
	URL     string
	Headers map[string]string
	// TokenPassthrough sends the caller's credential of the turn as the only
	// Authorization, and refuses a request of a turn without one.
	TokenPassthrough bool
}

// Model is the provider endpoint the native process sends its model requests
// to, and the header that carries the API key there.
type Model struct {
	// URL is the base URL the native client would call, such as
	// https://api.openai.com/v1; request paths are joined onto it.
	URL string
	// Header and Prefix place the caller's bearer token the way the provider
	// reads an API key: X-Api-Key with no prefix for Anthropic, Authorization
	// with "Bearer " for OpenAI.
	Header, Prefix string
}

// Forwarder adds the caller's credential of the current turn to every request
// it forwards.
type Forwarder struct {
	token    string
	maxBody  int64
	listener net.Listener
	server   *http.Server
	targets  map[string]*mcpTarget
	model    *modelTarget

	mu         sync.Mutex
	credential string
}

type mcpTarget struct {
	url         *url.URL
	headers     map[string]string
	passthrough bool
	proxy       *httputil.ReverseProxy
}

type modelTarget struct {
	url            *url.URL
	header, prefix string
	proxy          *httputil.ReverseProxy
}

// New binds an authenticated loopback endpoint per MCP server, and one for the
// model. Only streamable HTTP MCP servers are supported: an SSE server
// announces its message endpoint from the upstream host, which the forwarder
// does not rewrite. maxMCPBodyBytes bounds an MCP request body; a model
// request is bounded by its provider.
func New(routes Routes, maxMCPBodyBytes int) (*Forwarder, error) {
	if len(routes.MCPServers) == 0 && routes.Model == nil || maxMCPBodyBytes <= 0 {
		return nil, fmt.Errorf("a route and a positive MCP body limit are required")
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate credential forwarder token: %w", err)
	}
	forwarder := &Forwarder{
		token: hex.EncodeToString(tokenBytes), maxBody: int64(maxMCPBodyBytes),
		targets: make(map[string]*mcpTarget, len(routes.MCPServers)),
	}
	for name, server := range routes.MCPServers {
		if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "/?#") {
			return nil, fmt.Errorf("MCP server name %q cannot be forwarded", name)
		}
		target, err := upstreamURL(server.URL)
		if err != nil {
			return nil, fmt.Errorf("MCP server %q: %w", name, err)
		}
		headers := make(map[string]string, len(server.Headers))
		for header, value := range server.Headers {
			headers[http.CanonicalHeaderKey(header)] = value
		}
		entry := &mcpTarget{url: target, headers: headers, passthrough: server.TokenPassthrough}
		entry.proxy = &httputil.ReverseProxy{Rewrite: entry.rewrite, FlushInterval: -1}
		forwarder.targets[name] = entry
	}
	if model := routes.Model; model != nil {
		target, err := upstreamURL(model.URL)
		if err != nil {
			return nil, fmt.Errorf("model: %w", err)
		}
		if strings.TrimSpace(model.Header) == "" {
			return nil, fmt.Errorf("model API key header is required")
		}
		forwarder.model = &modelTarget{url: target, header: http.CanonicalHeaderKey(model.Header), prefix: model.Prefix}
		forwarder.model.proxy = &httputil.ReverseProxy{Rewrite: forwarder.model.rewrite, FlushInterval: -1}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for forwarded calls: %w", err)
	}
	forwarder.listener = listener
	forwarder.server = &http.Server{
		Handler:           forwarder.authorize(http.HandlerFunc(forwarder.serve)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = forwarder.server.Serve(listener) }()
	return forwarder, nil
}

func upstreamURL(raw string) (*url.URL, error) {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return nil, fmt.Errorf("URL %q must be an absolute http(s) URL", raw)
	}
	return target, nil
}

// MCPURL is the loopback endpoint the native process uses for the named server.
func (f *Forwarder) MCPURL(name string) string {
	return "http://" + f.listener.Addr().String() + mcpPathPrefix + name
}

// ModelURL is the loopback base URL the native process uses for the model.
func (f *Forwarder) ModelURL() string {
	return "http://" + f.listener.Addr().String() + modelPath
}

// Token is the per-process secret that authenticates the native process to the
// loopback endpoint, sent as its model API key or as a bearer token.
func (f *Forwarder) Token() string { return f.token }

// Authorization is the header value that authenticates the native process to
// the loopback endpoint.
func (f *Forwarder) Authorization() string { return "Bearer " + f.token }

// Bind sets the caller's credential every forwarded request carries until the
// returned function drops it.
func (f *Forwarder) Bind(credential string) (release func()) {
	f.mu.Lock()
	f.credential = credential
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.credential = ""
		f.mu.Unlock()
	}
}

// Close stops the loopback listener.
func (f *Forwarder) Close() error { return f.server.Close() }

func (f *Forwarder) current() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.credential
}

// authorize admits a request that carries the loopback token as a bearer
// token or, as an Anthropic client sends its API key, in X-Api-Key.
func (f *Forwarder) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		bearer := subtle.ConstantTimeCompare([]byte(f.Authorization()), []byte(request.Header.Get("Authorization"))) == 1
		apiKey := subtle.ConstantTimeCompare([]byte(f.token), []byte(request.Header.Get("X-Api-Key"))) == 1
		if !bearer && !apiKey {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (f *Forwarder) serve(response http.ResponseWriter, request *http.Request) {
	// The suffix is joined onto the upstream URL, which resolves dot segments:
	// a request must not climb out of its server's path with the credential.
	if !forwardablePath(request) {
		http.Error(response, "path holds a dot segment, an encoded slash or a character the forwarder refuses", http.StatusBadRequest)
		return
	}
	if suffix, ok := modelSuffix(request.URL.Path); ok && f.model != nil {
		f.serveModel(response, request, suffix)
		return
	}
	rest, ok := strings.CutPrefix(request.URL.Path, mcpPathPrefix)
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
	credential := f.current()
	if target.passthrough && credential == "" {
		http.Error(response, "MCP token passthrough requires a caller bearer token", http.StatusUnauthorized)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, f.maxBody)
	request = request.WithContext(context.WithValue(request.Context(), forwardInfoKey{}, forwardInfo{suffix: suffix, credential: credential}))
	target.proxy.ServeHTTP(response, request)
}

// serveModel forwards a model request with the caller's bearer token as the
// API key, as the Go ADK does under tokenPassthrough. A turn without one is
// refused rather than sent with the loopback token.
func (f *Forwarder) serveModel(response http.ResponseWriter, request *http.Request, suffix string) {
	token := bearerToken(f.current())
	if token == "" {
		http.Error(response, "model token passthrough requires a caller bearer token", http.StatusUnauthorized)
		return
	}
	request = request.WithContext(context.WithValue(request.Context(), forwardInfoKey{}, forwardInfo{suffix: suffix, credential: token}))
	f.model.proxy.ServeHTTP(response, request)
}

func modelSuffix(path string) (string, bool) {
	if path == modelPath {
		return "", true
	}
	return strings.CutPrefix(path, modelPath+"/")
}

// bearerToken returns the token of a Bearer authorization, or the empty string.
func bearerToken(authorization string) string {
	scheme, token, ok := strings.Cut(strings.TrimSpace(authorization), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

// forwardInfo is what serve decided for one request: the path under the
// server's endpoint, and the credential read once so the refusal above and
// the header below agree.
type forwardInfo struct {
	suffix     string
	credential string
}

type forwardInfoKey struct{}

func (t *mcpTarget) rewrite(proxied *httputil.ProxyRequest) {
	out := proxied.Out
	info, _ := proxied.In.Context().Value(forwardInfoKey{}).(forwardInfo)
	setUpstream(out, t.url, info.suffix, proxied.In.URL.RawQuery)
	// The loopback token authenticates the native process to this one only,
	// and the native process chooses no other header the caller's credential
	// travels with.
	out.Header = transportHeaders(proxied.In.Header)
	// The caller's credential first and the compiled headers last, so a
	// static Authorization wins, as in the Go ADK. A passthrough server
	// carries the caller's credential only; it compiles no static one.
	if info.credential != "" {
		out.Header.Set("Authorization", info.credential)
	}
	for header, value := range t.headers {
		out.Header.Set(header, value)
	}
	if t.passthrough {
		out.Header.Set("Authorization", info.credential)
	}
}

// forwardablePath reports whether a request path has no "." or ".." segment,
// decoded or not, no encoded slash, no backslash and no encoded percent sign,
// which an upstream decoding once more would turn into one of the others. The
// raw request target is checked as well as the parsed path: url.URL drops a
// RawPath it cannot reproduce, and EscapedPath then re-encodes the decoded path,
// in which an encoded slash is already a separator. A ";", which some servers
// strip with what follows it in a segment, and control bytes are refused too.
func forwardablePath(request *http.Request) bool {
	rawPath, _, _ := strings.Cut(request.RequestURI, "?")
	for _, escaped := range []string{rawPath, request.URL.EscapedPath()} {
		escaped = strings.ToLower(escaped)
		for _, refused := range []string{"%2f", "%5c", "%25", "\\"} {
			if strings.Contains(escaped, refused) {
				return false
			}
		}
	}
	if strings.ContainsFunc(request.URL.Path, func(r rune) bool { return r == ';' || r < 0x20 || r == 0x7f }) {
		return false
	}
	for segment := range strings.SplitSeq(request.URL.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// credentialHeaders are the request headers a model client authenticates with.
// The native process sends the loopback token in one of them, so none is
// forwarded; the provider's API key header is set from the caller's token.
var credentialHeaders = []string{"Authorization", "X-Api-Key", "Api-Key", "Proxy-Authorization", "Cookie"}

// rewrite keeps the native client's other headers, because provider APIs
// select versions and features through them.
func (t *modelTarget) rewrite(proxied *httputil.ProxyRequest) {
	out := proxied.Out
	info, _ := proxied.In.Context().Value(forwardInfoKey{}).(forwardInfo)
	setUpstream(out, t.url, info.suffix, proxied.In.URL.RawQuery)
	for _, header := range credentialHeaders {
		out.Header.Del(header)
	}
	out.Header.Set(t.header, t.prefix+info.credential)
}

// mcpTransportHeaders are the headers of the native process's request that
// reach an MCP server: what the streamable HTTP transport and trace
// propagation need.
var mcpTransportHeaders = []string{
	"Accept", "Content-Type", "Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-Id",
	"User-Agent", "Traceparent", "Tracestate", "Baggage",
}

func transportHeaders(in http.Header) http.Header {
	out := make(http.Header, len(mcpTransportHeaders))
	for _, header := range mcpTransportHeaders {
		if values := in.Values(header); len(values) != 0 {
			out[header] = slices.Clone(values)
		}
	}
	return out
}

// setUpstream points the outgoing request at the upstream URL joined with the
// path under the loopback route and both queries.
func setUpstream(out *http.Request, target *url.URL, suffix, query string) {
	out.URL.Scheme = target.Scheme
	out.URL.Host = target.Host
	out.Host = target.Host
	out.URL.Path, out.URL.RawPath = joinPath(target, suffix)
	out.URL.RawQuery = joinQuery(target.RawQuery, query)
}

func joinPath(target *url.URL, suffix string) (path, rawPath string) {
	if suffix == "" {
		return target.Path, target.RawPath
	}
	joined := target.JoinPath(suffix)
	// JoinPath keeps a pathless URL relative, which is not a valid request line.
	if !strings.HasPrefix(joined.Path, "/") {
		joined.Path = "/" + joined.Path
		if joined.RawPath != "" {
			joined.RawPath = "/" + joined.RawPath
		}
	}
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

// ResolveHeaders replaces each header value that is a whole ${NAME} reference
// with that variable of the environment, the form both compilers give a
// Secret-backed header, so the forwarder sends what the native client would
// have sent.
func ResolveHeaders(headers map[string]string, environment []string) (map[string]string, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	resolved := make(map[string]string, len(headers))
	for header, value := range headers {
		name, opened := strings.CutPrefix(value, "${")
		name, closed := strings.CutSuffix(name, "}")
		if !opened || !closed {
			resolved[header] = value
			continue
		}
		variable, found := lookup(environment, name)
		if !found || variable == "" {
			return nil, fmt.Errorf("header %q references environment variable %q, which is unset", header, name)
		}
		resolved[header] = variable
	}
	return resolved, nil
}

func lookup(environment []string, name string) (string, bool) {
	value, found := "", false
	for _, entry := range environment {
		if variable, ok := strings.CutPrefix(entry, name+"="); ok {
			value, found = variable, true
		}
	}
	return value, found
}
