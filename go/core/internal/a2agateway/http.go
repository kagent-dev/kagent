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
	rpc := withHTTPAgent(a2asrv.NewJSONRPCHandler(gateway))
	mux.Handle("POST "+HTTPPathPrefix+"{namespace}/{name}", rpc)
	mux.Handle("POST "+HTTPPathPrefix+"{namespace}/{name}/{$}", rpc)
	mux.Handle("GET "+HTTPPathPrefix+"{namespace}/{name}"+a2asrv.WellKnownAgentCardPath, withHTTPAgent(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		card, err := gateway.GetExtendedAgentCard(r.Context(), &a2atype.GetExtendedAgentCardRequest{})
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
	})))
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

func withHTTPAgent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), httpAgentKey{}, r.PathValue("namespace")+"/"+r.PathValue("name"))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
