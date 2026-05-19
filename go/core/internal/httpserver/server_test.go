package httpserver

import "testing"

func TestIsA2ARequestPath(t *testing.T) {
	tests := []struct {
		name        string
		escapedPath string
		want        bool
	}{
		{name: "agent route", escapedPath: "/api/a2a/kagent/agent", want: true},
		{name: "agent subroute", escapedPath: "/api/a2a/kagent/agent/tasks", want: true},
		{name: "sandbox route", escapedPath: "/api/a2a-sandboxes/kagent/sandbox", want: true},
		{name: "non a2a api", escapedPath: "/api/me", want: false},
		{name: "evil agent prefix", escapedPath: "/api/a2aevil/kagent/agent", want: false},
		{name: "evil sandbox prefix", escapedPath: "/api/a2a-sandboxesevil/kagent/sandbox", want: false},
		{name: "missing name segment", escapedPath: "/api/a2a/kagent", want: false},
		{name: "encoded slash", escapedPath: "/api/a2a/kagent%2Fagent/tasks", want: false},
		{name: "encoded slash lowercase", escapedPath: "/api/a2a/kagent%2fagent/tasks", want: false},
		{name: "encoded backslash", escapedPath: "/api/a2a/kagent%5Cagent/tasks", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isA2ARequestPath(tt.escapedPath); got != tt.want {
				t.Fatalf("isA2ARequestPath(%q) = %v, want %v", tt.escapedPath, got, tt.want)
			}
		})
	}
}
