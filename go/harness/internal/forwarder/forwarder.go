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

const mcpPathPrefix = "/mcp/"

// MCPServer is one compiled MCP server the forwarder fronts: its real URL and
// the static headers the compiler assigned to it, with their values resolved.
type MCPServer struct {
	URL     string
	Headers map[string]string
	// TokenPassthrough sends the caller's credential of the turn as the only
	// Authorization, and refuses a request of a turn without one.
	TokenPassthrough bool
}

// Forwarder adds the caller's credential of the current turn to every request
// it forwards.
type Forwarder struct {
	token    string
	maxBody  int64
	listener net.Listener
	server   *http.Server
	targets  map[string]*mcpTarget

	mu         sync.Mutex
	credential string
}

type mcpTarget struct {
	url         *url.URL
	headers     map[string]string
	passthrough bool
	proxy       *httputil.ReverseProxy
}

// New binds an authenticated loopback endpoint per MCP server. Only
// streamable HTTP servers are supported: an SSE server announces its message
// endpoint from the upstream host, which the forwarder does not rewrite.
func New(servers map[string]MCPServer, maxBodyBytes int) (*Forwarder, error) {
	if len(servers) == 0 || maxBodyBytes <= 0 {
		return nil, fmt.Errorf("MCP servers and a positive body limit are required")
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate credential forwarder token: %w", err)
	}
	forwarder := &Forwarder{
		token: hex.EncodeToString(tokenBytes), maxBody: int64(maxBodyBytes),
		targets: make(map[string]*mcpTarget, len(servers)),
	}
	for name, server := range servers {
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
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for forwarded calls: %w", err)
	}
	forwarder.listener = listener
	forwarder.server = &http.Server{
		Handler:           forwarder.authorizeAndLimit(http.HandlerFunc(forwarder.serve)),
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

func (f *Forwarder) authorizeAndLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		want, got := f.Authorization(), request.Header.Get("Authorization")
		if subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		request.Body = http.MaxBytesReader(response, request.Body, f.maxBody)
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
	request = request.WithContext(context.WithValue(request.Context(), forwardInfoKey{}, forwardInfo{suffix: suffix, credential: credential}))
	target.proxy.ServeHTTP(response, request)
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
