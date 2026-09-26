package a2agateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// HTTPPathPrefix is the public A2A namespace on the core HTTP listener.
const HTTPPathPrefix = "/agents/"

// NewHTTPHandler serves a card and JSON-RPC endpoint per named Agent.
func NewHTTPHandler(gateway a2asrv.RequestHandler, authenticator auth.AuthProvider, shares sessionsvc.ShareStore) http.Handler {
	mux := http.NewServeMux()
	rpc := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bind this request's URL before entering the shared gateway. The SDK
		// decodes JSON-RPC and exposes any payload tenant to the interceptor.
		handler := &a2asrv.InterceptedHandler{
			Handler: gateway,
			Interceptors: []a2asrv.CallInterceptor{&httpAgentRoute{
				agent: r.PathValue("namespace") + "/" + r.PathValue("name"),
			}},
		}
		a2asrv.NewJSONRPCHandler(handler).ServeHTTP(w, r)
	})
	mux.Handle("POST "+HTTPPathPrefix+"{namespace}/{name}", rpc)
	mux.Handle("POST "+HTTPPathPrefix+"{namespace}/{name}/{$}", rpc)
	mux.Handle("GET "+HTTPPathPrefix+"{namespace}/{name}"+a2asrv.WellKnownAgentCardPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		card, err := gateway.GetExtendedAgentCard(r.Context(), &a2atype.GetExtendedAgentCardRequest{
			Tenant: r.PathValue("namespace") + "/" + r.PathValue("name"),
		})
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, a2atype.ErrUnauthenticated):
				status = http.StatusUnauthorized
			case errors.Is(err, a2atype.ErrUnauthorized):
				status = http.StatusForbidden
			case errors.Is(err, a2atype.ErrInvalidRequest):
				status = http.StatusBadRequest
			case errors.Is(err, a2atype.ErrUnsupportedOperation):
				status = http.StatusConflict
			}
			http.Error(w, http.StatusText(status), status)
			return
		}
		data, err := json.Marshal(card)
		if err != nil {
			logging.FromContext(r.Context()).ErrorContext(r.Context(), "failed to encode agent card", "error", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(data); err != nil {
			logging.FromContext(r.Context()).ErrorContext(r.Context(), "failed to write agent card", "error", err)
		}
	}))
	return auth.AuthnMiddleware(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		if _, ok := auth.AuthSessionFrom(r.Context()); !ok {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		share, err := sessionsvc.ResolveShare(r.Context(), shares, r.Header.Get("X-Share-Token"))
		if err != nil {
			status := http.StatusInternalServerError
			if serviceerrors.CodeOf(err) == serviceerrors.CodePermissionDenied {
				status = http.StatusForbidden
			}
			http.Error(w, http.StatusText(status), status)
			return
		}
		ctx := auth.ShareContextTo(r.Context(), share)
		mux.ServeHTTP(w, r.WithContext(ctx))
	}))
}

// httpAgentRoute normalizes the URL into the SDK's routing metadata. It runs
// after decoding: attaching a tenant before the SDK handles the request would
// allow a payload tenant to overwrite the Agent selected by the URL.
type httpAgentRoute struct {
	a2asrv.PassthroughCallInterceptor
	agent string
}

var _ a2asrv.CallInterceptor = (*httpAgentRoute)(nil)

func (h *httpAgentRoute) Before(ctx context.Context, callCtx *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
	if tenant := callCtx.Tenant(); tenant != "" && tenant != h.agent {
		return ctx, nil, a2atype.NewError(a2atype.ErrInvalidRequest, "tenant does not match the Agent URL")
	}
	return a2atype.AttachTenant(ctx, h.agent), nil, nil
}
