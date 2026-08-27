package grpcserver

import (
	"net/http"
	"strings"

	"github.com/improbable-eng/grpc-web/go/grpcweb"
)

// WebHandler exposes the same services over gRPC-Web.
//
// A browser cannot speak gRPC. The protocol needs HTTP/2 trailers, and `fetch`
// gives a page no way to read them, which is why the services registered in
// New() are unreachable from a page however the network is arranged. gRPC-Web
// carries the same frames over HTTP/1.1 with the trailers moved into the body,
// so this wrapper is what makes this API callable from a browser at all.
//
// Wrapping is deliberately all it does: the returned handler serves the very
// server built in New(), so a service registered there is reachable both ways
// and the interceptor chain — authentication included — runs identically for a
// call arriving either way. Nothing here decides who may call what.
//
// Most callers want WebHandlerOr, which also says what to do with everything
// that is not a gRPC-Web request.
func (s *Server) WebHandler() *grpcweb.WrappedGrpcServer {
	return grpcweb.WrapServer(s.server,
		// The UI is served from the same origin as this API in every deployment
		// the chart produces — nginx proxies /api to the controller — so no
		// cross-origin allowance is granted here. A deployment that genuinely
		// serves the two from different origins configures that on its ingress,
		// where the rest of its CORS policy already lives, rather than having
		// this server assert a policy it cannot see the whole of.
		grpcweb.WithCorsForRegisteredEndpointsOnly(true),
	)
}

// WebHandlerOr routes gRPC-Web requests to the services and everything else to next.
//
// This is the one statement of the rule, because there is more than one binary
// serving HTTP beside this gRPC server and a second copy would drift. Both the
// v1 controller's HTTP server and the v2 controller's compose their handler
// through here.
//
// Two things it settles that are easy to get wrong:
//
// Requests are told apart by content type rather than by path, so the service
// names never have to be restated. And a leading `/api` is stripped, because the
// wrapper matches the gRPC path a generated client sends —
// `/<package>.<Service>/<Method>` — while the chart's nginx serves the whole API
// under /api on the UI's own origin. That prefix is where a same-origin browser
// has to address it from and nowhere the wrapper can be told about.
func (s *Server) WebHandlerOr(next http.Handler) http.Handler {
	web := s.WebHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !web.IsGrpcWebRequest(r) && !web.IsAcceptableGrpcCorsRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if trimmed, ok := trimAPIPrefix(r.URL.Path); ok {
			r = r.Clone(r.Context())
			r.URL.Path = trimmed
		}
		web.ServeHTTP(w, r)
	})
}

// trimAPIPrefix removes a leading /api, reporting whether there was one.
//
// A request that arrives without it — a client addressing the gRPC path directly
// — is passed through untouched rather than rejected, so the same server answers
// both spellings.
func trimAPIPrefix(path string) (string, bool) {
	const prefix = "/api"
	if !strings.HasPrefix(path, prefix+"/") {
		return path, false
	}
	return strings.TrimPrefix(path, prefix), true
}
