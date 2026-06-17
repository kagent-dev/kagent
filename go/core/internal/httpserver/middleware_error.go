package httpserver

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/jackc/pgx/v5"
	apierrors "github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

func errorHandlerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ew := &errorResponseWriter{
			ResponseWriter: w,
			request:        r,
		}

		next.ServeHTTP(ew, r)
	})
}

type errorResponseWriter struct {
	http.ResponseWriter
	request *http.Request
}

var _ handlers.ErrorResponseWriter = &errorResponseWriter{}

var _ http.Flusher = &errorResponseWriter{}

func (w *errorResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *errorResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacking not supported")
	}
	return hijacker.Hijack()
}

func (w *errorResponseWriter) RespondWithError(err error) {
	log := ctrllog.FromContext(w.request.Context())

	statusCode := http.StatusInternalServerError
	message := "Internal server error"

	if err == nil {
		err = errors.New("unknown error")
	}

	var underlying error
	if apiErr, ok := err.(*apierrors.APIError); ok {
		statusCode = apiErr.Code
		message = apiErr.Message
		underlying = apiErr.Err
	} else {
		underlying = err
	}

	if underlying != nil && !errors.Is(underlying, pgx.ErrNoRows) {
		log.Error(underlying, message)
	} else {
		log.Info(message)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	errMsg := message
	if underlying != nil {
		errMsg = message + ": " + underlying.Error()
	}
	json.NewEncoder(w).Encode(map[string]string{"error": errMsg}) //nolint:errcheck
}
