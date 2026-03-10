/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// RegisterBuiltinHandlers registers all built-in action handlers on the registry.
func RegisterBuiltinHandlers(r *ActionRegistry) {
	r.Register("noop", &NoopHandler{})
	r.Register("http.request", &HTTPRequestHandler{
		Client: http.DefaultClient,
	})
}

// NoopHandler returns inputs as outputs (for testing/placeholder steps).
type NoopHandler struct{}

// Execute returns the inputs as a JSON object output.
func (h *NoopHandler) Execute(_ context.Context, inputs map[string]string) (*ActionResult, error) {
	out, err := json.Marshal(inputs)
	if err != nil {
		return nil, fmt.Errorf("noop: failed to marshal inputs: %w", err)
	}
	return &ActionResult{Output: out}, nil
}

// HTTPRequestHandler makes HTTP requests.
type HTTPRequestHandler struct {
	Client *http.Client
}

// Execute makes an HTTP request based on the inputs.
// Supported input keys:
//   - url (required): the request URL
//   - method: HTTP method (default: GET)
//   - body: request body (for POST/PUT/PATCH)
//   - content_type: Content-Type header (default: application/json for requests with body)
func (h *HTTPRequestHandler) Execute(ctx context.Context, inputs map[string]string) (*ActionResult, error) {
	rawURL, ok := inputs["url"]
	if !ok || rawURL == "" {
		return &ActionResult{Error: "missing required input: url"}, nil
	}

	method := strings.ToUpper(inputs["method"])
	if method == "" {
		method = http.MethodGet
	}

	var bodyReader io.Reader
	if body, ok := inputs["body"]; ok && body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return &ActionResult{Error: fmt.Sprintf("failed to create request: %v", err)}, nil
	}

	if ct, ok := inputs["content_type"]; ok && ct != "" {
		req.Header.Set("Content-Type", ct)
	} else if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := h.Client.Do(req)
	if err != nil {
		return &ActionResult{Error: fmt.Sprintf("request failed: %v", err)}, nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &ActionResult{Error: fmt.Sprintf("failed to read response: %v", err)}, nil
	}

	output := map[string]interface{}{
		"status_code": resp.StatusCode,
		"body":        string(respBody),
	}
	out, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("http.request: failed to marshal response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return &ActionResult{
			Output: out,
			Error:  fmt.Sprintf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
		}, nil
	}

	return &ActionResult{Output: out}, nil
}
