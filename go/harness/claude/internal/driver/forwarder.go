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
// caller's credential of the current turn to every request it forwards to a
// server that configures no Authorization of its own. Claude receives the
// loopback address and a per-process token only; the caller's credential never
// enters its environment, its configuration files or the Actor's filesystem,
// and it is held in this process for one turn at a time.
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
	url                 *url.URL
	headers             map[string]string
	callerAuthorization bool
	proxy               *httputil.ReverseProxy
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
		// A server that configures Authorization owns it: the egress gateway
		// replaces its placeholder with the Secret, and only when the request
		// carries one. Every other server takes the caller's credential.
		_, ownsAuthorization := headers["Authorization"]
		entry := &forwardTarget{url: target, headers: headers, callerAuthorization: !ownsAuthorization}
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
	// The suffix is joined onto the upstream URL, which resolves dot segments:
	// a request must not climb out of its server's path with the credential.
	if !forwardablePath(request) {
		http.Error(response, "path holds a dot segment, an encoded slash or a character the forwarder refuses", http.StatusBadRequest)
		return
	}
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
		// The loopback token authenticates Claude to this process only, and
		// Claude chooses no other header the caller's credential travels with.
		out.Header = forwardedHeaders(proxied.In.Header)
		for header, value := range target.headers {
			out.Header.Set(header, value)
		}
		if !target.callerAuthorization {
			return
		}
		// A turn without a caller credential reaches the upstream without
		// Authorization, never with another credential in its place.
		if credential := f.current(); credential != "" {
			out.Header.Set("Authorization", credential)
		}
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

// forwardedRequestHeaders are the headers of Claude's request that reach the
// upstream: what the streamable HTTP transport and trace propagation need.
var forwardedRequestHeaders = []string{
	"Accept", "Content-Type", "Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-Id",
	"User-Agent", "Traceparent", "Tracestate", "Baggage",
}

func forwardedHeaders(in http.Header) http.Header {
	out := make(http.Header, len(forwardedRequestHeaders))
	for _, header := range forwardedRequestHeaders {
		if values := in.Values(header); len(values) != 0 {
			out[header] = slices.Clone(values)
		}
	}
	return out
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
