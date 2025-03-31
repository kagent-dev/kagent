package httpserver

import (
	"encoding/json"
	"net/http"

	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/handlers"
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

func (w *errorResponseWriter) RespondWithError(err error) {
	log := ctrllog.FromContext(w.request.Context())

	statusCode := http.StatusInternalServerError
	message := "Internal server error"

	if apiErr, ok := err.(*errors.APIError); ok {
		statusCode = apiErr.Code
		message = apiErr.Message
		log.Error(apiErr.Err, message)
	} else {
		log.Error(err, "Unhandled error")
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
