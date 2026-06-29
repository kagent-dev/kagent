package httpserver

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		log := ctrllog.Log.WithName("http").WithValues(
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
		)

		if userID := r.URL.Query().Get("user_id"); userID != "" {
			log = log.WithValues("user_id", userID)
		}

		ww := newStatusResponseWriter(w)
		ctx := ctrllog.IntoContext(r.Context(), log)
		log.V(1).Info("Request started")
		next.ServeHTTP(ww, r.WithContext(ctx))
		log.Info("Request completed",
			"status", ww.status,
			"duration", time.Since(start),
		)
	})
}

// For streaming responses in A2A lib
var _ http.Flusher = &statusResponseWriter{}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func newStatusResponseWriter(w http.ResponseWriter) *statusResponseWriter {
	return &statusResponseWriter{w, http.StatusOK}
}

func (w *statusResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacking not supported")
	}
	return hijacker.Hijack()
}

// Forward RespondWithError to underlying writer if it implements ErrorResponseWriter
func (w *statusResponseWriter) RespondWithError(err error) {
	if errWriter, ok := w.ResponseWriter.(handlers.ErrorResponseWriter); ok {
		errWriter.RespondWithError(err)
		w.status = 500
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func contentTypeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
			w.Header().Set("Content-Type", "application/json")
		}
		next.ServeHTTP(w, r)
	})
}

// shareTokenMiddleware validates X-Share-Token headers.
// It runs after the auth middleware, so the caller is already authenticated.
// When the header is present and resolves to a valid share record, a ShareContext
// is stored on the request context so that session handlers can use the owner's
// user ID for DB lookups while retaining the caller's identity for initiated_by tracking.
func (s *HTTPServer) shareTokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Share-Token")
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		_, ok := auth.AuthSessionFrom(r.Context())
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		share, err := s.config.DbClient.GetSessionShareByToken(r.Context(), token)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "Invalid or expired share token", http.StatusForbidden)
			} else {
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
			return
		}

		// Enforce read-only on session and A2A paths only. Visitors retain full
		// authenticated access to all other endpoints (creating their own sessions,
		// submitting feedback, etc.) — the share token should not restrict unrelated operations.
		if share.ReadOnly && r.Method != http.MethodGet && r.Method != http.MethodHead {
			path := r.URL.Path
			if strings.HasPrefix(path, APIPathSessions+"/") ||
				strings.HasPrefix(path, APIPathA2A+"/") ||
				strings.HasPrefix(path, APIPathA2ASandboxes+"/") {
				http.Error(w, "This share link is read-only", http.StatusForbidden)
				return
			}
		}

		callerSession, _ := auth.AuthSessionFrom(r.Context())
		callerID := callerSession.Principal().User.ID
		if err := s.config.DbClient.RecordShareAccess(r.Context(), callerID, share.ID); err != nil {
			log := ctrllog.FromContext(r.Context())
			log.Error(err, "failed to record share access", "shareID", share.ID)
		}

		sc := &auth.ShareContext{
			Token:     token,
			SessionID: share.SessionID,
			UserID:    share.UserID,
			ReadOnly:  share.ReadOnly,
		}
		r = r.WithContext(auth.ShareContextTo(r.Context(), sc))
		next.ServeHTTP(w, r)
	})
}
