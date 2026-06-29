package tools

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseAppName(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantNamespace string
		wantName      string
	}{
		{
			name:          "standard format with underscores",
			input:         "kagent__NS__my_agent",
			wantNamespace: "kagent",
			wantName:      "my-agent",
		},
		{
			name:          "custom namespace and agent name",
			input:         "default__NS__test_agent",
			wantNamespace: "default",
			wantName:      "test-agent",
		},
		{
			name:          "no separator returns empty namespace",
			input:         "noseperator",
			wantNamespace: "",
			wantName:      "noseperator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNamespace, gotName := parseAppName(tt.input)
			if gotNamespace != tt.wantNamespace {
				t.Errorf("parseAppName(%q) namespace = %q, want %q", tt.input, gotNamespace, tt.wantNamespace)
			}
			if gotName != tt.wantName {
				t.Errorf("parseAppName(%q) name = %q, want %q", tt.input, gotName, tt.wantName)
			}
		})
	}
}

func TestShareClient_ShareURL_WithUIURL(t *testing.T) {
	c := &shareClient{
		baseURL: "http://localhost",
		uiURL:   "https://example.com",
		appName: "kagent__NS__myagent",
	}

	got := c.shareURL("abc123", "sess-1")
	want := "https://example.com/agents/kagent/myagent/chat/sess-1?share=abc123"
	if got != want {
		t.Errorf("shareURL() = %q, want %q", got, want)
	}
}

func TestShareClient_ShareURL_WithoutUIURL(t *testing.T) {
	c := &shareClient{
		baseURL: "http://localhost",
		uiURL:   "",
		appName: "kagent__NS__myagent",
	}

	got := c.shareURL("abc123", "sess-1")
	want := "/agents/kagent/myagent/chat/sess-1?share=abc123"
	if got != want {
		t.Errorf("shareURL() = %q, want %q", got, want)
	}
}

func TestNewShareTools_HaveCorrectNames(t *testing.T) {
	tests := []struct {
		toolName    string
		constructor func(*http.Client, string, string) (interface{ Name() string }, error)
	}{
		{
			toolName: "create_share_link",
			constructor: func(c *http.Client, base, app string) (interface{ Name() string }, error) {
				return NewCreateShareLinkTool(c, base, app)
			},
		},
		{
			toolName: "list_share_links",
			constructor: func(c *http.Client, base, app string) (interface{ Name() string }, error) {
				return NewListShareLinksTool(c, base, app)
			},
		},
		{
			toolName: "delete_share_link",
			constructor: func(c *http.Client, base, app string) (interface{ Name() string }, error) {
				return NewDeleteShareLinkTool(c, base, app)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			tool, err := tt.constructor(http.DefaultClient, "http://localhost", "test__NS__app")
			if err != nil {
				t.Fatalf("constructor for %q returned error: %v", tt.toolName, err)
			}
			if tool.Name() != tt.toolName {
				t.Errorf("tool.Name() = %q, want %q", tool.Name(), tt.toolName)
			}
		})
	}
}

func TestShareClient_DoWithJSON_SendsCorrectHeaders(t *testing.T) {
	var capturedReq *http.Request
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := &shareClient{
		baseURL:    server.URL,
		appName:    "test-app",
		httpClient: server.Client(),
	}

	resp, err := c.doWithJSON(context.Background(), "POST", "/test", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("doWithJSON() error = %v", err)
	}
	defer resp.Body.Close()

	if got := capturedReq.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type header = %q, want %q", got, "application/json")
	}

	if got := capturedReq.Header.Get("X-Agent-Name"); got != "test-app" {
		t.Errorf("X-Agent-Name header = %q, want %q", got, "test-app")
	}

	if string(capturedBody) != `{}` {
		t.Errorf("request body = %q, want %q", string(capturedBody), `{}`)
	}
}
