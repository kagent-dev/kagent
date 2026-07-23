package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	apierrors "github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/stretchr/testify/require"
)

func TestRespondWithErrorBody(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantError  string
	}{
		{
			name:       "4xx keeps underlying detail",
			err:        apierrors.NewBadRequestError("invalid request", errors.New("name is required")),
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request: name is required",
		},
		{
			name:       "5xx hides underlying detail",
			err:        apierrors.NewInternalServerError("Internal server error", errors.New("connection refused")),
			wantStatus: http.StatusInternalServerError,
			wantError:  "Internal server error",
		},
		{
			name:       "bare error defaults to 500 without detail",
			err:        errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantError:  "Internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writer := &errorResponseWriter{
				ResponseWriter: recorder,
				request:        httptest.NewRequest(http.MethodGet, "/", nil).WithContext(t.Context()),
			}

			writer.RespondWithError(tt.err)

			require.Equal(t, tt.wantStatus, recorder.Code)
			var body map[string]string
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.Equal(t, tt.wantError, body["error"])
		})
	}
}
