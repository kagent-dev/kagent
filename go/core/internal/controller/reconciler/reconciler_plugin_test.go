package reconciler

import (
	"testing"
)

func TestDeriveBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "URL with path",
			input: "http://kanban-mcp.kagent.svc:8080/mcp",
			want:  "http://kanban-mcp.kagent.svc:8080",
		},
		{
			name:  "URL without path",
			input: "http://kanban-mcp.kagent.svc:8080",
			want:  "http://kanban-mcp.kagent.svc:8080",
		},
		{
			name:  "URL with deep path",
			input: "http://host:9090/path/to/mcp",
			want:  "http://host:9090",
		},
		{
			name:  "URL with query and fragment",
			input: "http://host:8080/mcp?key=val#frag",
			want:  "http://host:8080",
		},
		{
			name:  "HTTPS URL",
			input: "https://secure-mcp.example.com/mcp",
			want:  "https://secure-mcp.example.com",
		},
		{
			name:    "invalid URL",
			input:   "://invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := deriveBaseURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("deriveBaseURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("deriveBaseURL() = %v, want %v", got, tt.want)
			}
		})
	}
}
