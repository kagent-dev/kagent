package handlers_test

import (
	"net/http"
	"net/http/httptest"

	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
)

type mockErrorResponseWriter struct {
	*httptest.ResponseRecorder
	errorReceived error
}

func newMockErrorResponseWriter() *mockErrorResponseWriter {
	return &mockErrorResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

func (m *mockErrorResponseWriter) RespondWithError(err error) {
	m.errorReceived = err

	if errWithStatus, ok := err.(interface{ StatusCode() int }); ok {
		handlers.RespondWithError(m, errWithStatus.StatusCode(), err.Error())
	} else {
		handlers.RespondWithError(m, http.StatusInternalServerError, err.Error())
	}
}
