package handlers

import (
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
)

func setupScheme() *runtime.Scheme {
	s := scheme.Scheme

	s.AddKnownTypes(schema.GroupVersion{Group: "kagent.dev", Version: "v1alpha1"},
		&v1alpha1.Agent{},
		&v1alpha1.AgentList{},
		&v1alpha1.ModelConfig{},
		&v1alpha1.ModelConfigList{},
	)

	metav1.AddToGroupVersion(s, schema.GroupVersion{Group: "kagent.dev", Version: "v1alpha1"})

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
