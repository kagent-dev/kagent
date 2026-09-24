package a2agateway

import (
	"encoding/json"
	"errors"
	"net/http"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/core/internal/service/agentinstance"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"google.golang.org/grpc/metadata"
)

// HTTPPathPrefix is the public A2A namespace on the core HTTP listener.
const HTTPPathPrefix = "/agents/"

// NewHTTPHandler serves a card and JSON-RPC endpoint per AgentInstance. The URL
// selects the instance; caller-supplied routing headers cannot override it.
func NewHTTPHandler(gateway a2asrv.RequestHandler, authenticator auth.AuthProvider, shares agentinstance.ShareStore) http.Handler {
	mux := http.NewServeMux()
	rpc := withHTTPInstance(a2asrv.NewJSONRPCHandler(gateway))
	mux.Handle("POST "+HTTPPathPrefix+"{instanceID}", rpc)
	mux.Handle("POST "+HTTPPathPrefix+"{instanceID}/{$}", rpc)
	mux.Handle("GET "+HTTPPathPrefix+"{instanceID}"+a2asrv.WellKnownAgentCardPath, withHTTPInstance(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		share, err := agentinstance.ResolveShare(r.Context(), shares, r.Header.Get("X-Share-Token"))
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

func withHTTPInstance(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Normalize the URL into the same routing metadata used by gRPC.
		ctx := metadata.NewIncomingContext(r.Context(), metadata.Pairs(apia2a.AgentInstanceIDHeader, r.PathValue("instanceID")))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
