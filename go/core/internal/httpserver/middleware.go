package httpserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	"github.com/kagent-dev/kagent/go/core/internal/scheduledrun"
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

// reservedScheduledRunUserMiddleware prevents an external human identity from
// colliding with the internal user ID used to persist ScheduledRun state.
// Agent callbacks are allowed because they carry an authenticated Agent ID and
// need the reserved user ID to write events and tasks for the scheduled session.
func reservedScheduledRunUserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if session, ok := auth.AuthSessionFrom(r.Context()); ok {
			principal := session.Principal()
			if principal.User.ID == scheduledrun.SessionUserID && principal.Agent.ID == "" {
				http.Error(w, "Reserved user identity", http.StatusForbidden)
				return
			}
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
			if errors.Is(err, database.ErrNotFound) {
				http.Error(w, "Invalid or expired share token", http.StatusForbidden)
			} else {
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
			return
		}

		// Enforce read-only on the session REST path by HTTP verb. A2A traffic is
		// JSON-RPC over POST, so the verb does not distinguish reads from writes;
		// its read-only enforcement is per-method in the A2A request handler
		// (requireWritableShare), which lets a read-only share list and get tasks
		// while still rejecting message sends, cancels, and push-config writes.
		// Visitors retain full authenticated access to all other endpoints
		// (creating their own sessions, submitting feedback, etc.).
		if share.ReadOnly && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if strings.HasPrefix(r.URL.Path, APIPathSessions+"/") {
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

func a2aRequestContextID(r *http.Request) string {
	if r.Body == nil || r.Method != http.MethodPost {
		return ""
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return ""
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var payload struct {
		Params struct {
			ContextID string `json:"contextId"`
			Message   struct {
				ContextID string `json:"contextId"`
			} `json:"message"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if contextID := strings.TrimSpace(payload.Params.Message.ContextID); contextID != "" {
		return contextID
	}
	return strings.TrimSpace(payload.Params.ContextID)
}

// isA2ARequestPath reports whether the path targets an A2A agent or sandbox
// route, the only routes carrying ScheduledRun-owned sessions.
func isA2ARequestPath(path string) bool {
	return strings.HasPrefix(path, APIPathA2A+"/") ||
		strings.HasPrefix(path, APIPathA2ASandboxes+"/")
}

// scheduledRunSessionMiddleware maps A2A requests for ScheduledRun-owned
// sessions onto the same ShareContext used by session sharing. This reuses the
// existing per-method read-only enforcement and owner-scoped task lookups.
func (s *HTTPServer) scheduledRunSessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, alreadyShared := auth.ShareContextFrom(r.Context())
		if alreadyShared || !isA2ARequestPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		sessionID := a2aRequestContextID(r)
		if sessionID == "" || s.handlers == nil || s.handlers.Sessions == nil || s.config.DbClient == nil {
			next.ServeHTTP(w, r)
			return
		}
		access, isScheduledRunSession, err := s.handlers.Sessions.ResolveScheduledRunSessionAccess(r, sessionID)
		if err != nil {
			switch {
			case errors.Is(err, handlers.ErrScheduledRunSessionForbidden):
				http.Error(w, "Forbidden", http.StatusForbidden)
			case errors.Is(err, handlers.ErrScheduledRunSessionNotFound):
				http.Error(w, "ScheduledRun session not found", http.StatusNotFound)
			default:
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
			return
		}
		if !isScheduledRunSession {
			next.ServeHTTP(w, r)
			return
		}
		expectedPath := APIPathA2A
		if access.TargetKind == scheduledrun.TargetKindSandboxAgent {
			expectedPath = APIPathA2ASandboxes
		}
		vars := mux.Vars(r)
		if !strings.HasPrefix(r.URL.Path, expectedPath+"/") ||
			vars["namespace"] != access.Target.Namespace ||
			vars["name"] != access.Target.Name {
			http.Error(w, "ScheduledRun session cannot be used with this target", http.StatusForbidden)
			return
		}
		r = r.WithContext(auth.ShareContextTo(r.Context(), &auth.ShareContext{
			SessionID: sessionID,
			UserID:    access.UserID,
			ReadOnly:  access.ReadOnly,
		}))
		next.ServeHTTP(w, r)
	})
}
