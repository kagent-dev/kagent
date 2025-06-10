package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvokeAPI(t *testing.T) {
	t.Run("when sent to the server", func(t *testing.T) {
		var apiURL string

		// Setup
		apiURL = os.Getenv("KAGENT_API_URL")
		if apiURL == "" {
			apiURL = "http://localhost:8001"
		}

		t.Run("should successfully handle a synchronous invocation", func(t *testing.T) {
			payload := map[string]interface{}{
				"message": "Test message from integration test",
				"user_id": "integration-test-user",
			}
			payloadBytes, err := json.Marshal(payload)
			require.NoError(t, err)

			client := http.Client{
				Timeout: time.Second * 30,
			}

			req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/agents/1/invoke", apiURL), bytes.NewBuffer(payloadBytes))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			req = req.WithContext(ctx)

			resp, err := client.Do(req)
			if err != nil {
				t.Skipf("Server not available: %s", err.Error())
				return
			}
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)

			var responseData map[string]interface{}
			err = json.NewDecoder(resp.Body).Decode(&responseData)
			require.NoError(t, err)

			assert.Contains(t, responseData, "sessionId")
			assert.Contains(t, responseData, "response")
			assert.Contains(t, responseData, "status")
			assert.Equal(t, "completed", responseData["status"])
		})

		t.Run("should successfully handle an asynchronous invocation", func(t *testing.T) {
			payload := map[string]interface{}{
				"message": "Test message from integration test",
				"user_id": "integration-test-user",
			}
			payloadBytes, err := json.Marshal(payload)
			require.NoError(t, err)

			client := http.Client{
				Timeout: time.Second * 30,
			}

			req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/agents/1/start", apiURL), bytes.NewBuffer(payloadBytes))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			req = req.WithContext(ctx)

			resp, err := client.Do(req)
			if err != nil {
				t.Skipf("Server not available: %s", err.Error())
				return
			}
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)

			var responseData map[string]interface{}
			err = json.NewDecoder(resp.Body).Decode(&responseData)
			require.NoError(t, err)

			assert.Contains(t, responseData, "sessionId")
			assert.Contains(t, responseData, "statusUrl")
			assert.Contains(t, responseData, "status")
			assert.Equal(t, "processing", responseData["status"])
		})
	})
}
