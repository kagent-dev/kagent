package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"

	apierrors "github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
	"gorm.io/gorm"
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

func (w *errorResponseWriter) RespondWithError(err error) {
	log := ctrllog.FromContext(w.request.Context())

	statusCode := http.StatusInternalServerError
	message := "Internal server error"

	if err == nil {
		err = errors.New("unknown error")
	}

	if apiErr, ok := err.(*apierrors.APIError); ok {
		statusCode = apiErr.Code
		message = apiErr.Message

		if apiErr.Err != nil {
			err = apiErr.Err
		}
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Error(err, message)
	} else {
		log.Info(message)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": message + ": " + err.Error()}) //nolint:errcheck
}
