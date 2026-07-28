package handlers_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	"github.com/stretchr/testify/require"
)

func TestRespondNotFoundOrError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "missing record maps to 404", err: fmt.Errorf("session x: %w", database.ErrNotFound), wantStatus: http.StatusNotFound},
		{name: "backend failure maps to 500", err: errors.New("connection refused"), wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newMockErrorResponseWriter()
			handlers.RespondNotFoundOrError(w, "not found", tt.err)
			require.Equal(t, tt.wantStatus, w.Code)
		})
	}
}
