package handlers_test

import (
	"net/http"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
)

func setupScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v1alpha2.AddToScheme(s)
	return s
}

type testErrorResponseWriter struct {
	http.ResponseWriter
}

func (t *testErrorResponseWriter) Flush() {
	if flusher, ok := t.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (t *testErrorResponseWriter) RespondWithError(err error) {
	if apiErr, ok := err.(*errors.APIError); ok {
		http.Error(t.ResponseWriter, apiErr.Message, apiErr.StatusCode())
	} else {
		http.Error(t.ResponseWriter, err.Error(), http.StatusInternalServerError)
	}
}

func (t *testErrorResponseWriter) WriteHeader(statusCode int) {
	t.ResponseWriter.WriteHeader(statusCode)
}
