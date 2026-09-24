package grpcserver

import (
	"testing"
)

// The prefix rule, which is the subtle half of HandlerOr.
func TestTrimAPIPrefix(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    string
		wantHad bool
	}{
		{"a same-origin browser call arrives under /api", "/api/kagent.api.v1alpha1.AgentInstanceService/ListAgentInstances", "/kagent.api.v1alpha1.AgentInstanceService/ListAgentInstances", true},
		{"a client addressing the gRPC path directly is untouched", "/kagent.api.v1alpha1.AgentInstanceService/ListAgentInstances", "/kagent.api.v1alpha1.AgentInstanceService/ListAgentInstances", false},
		{"/api alone is not a prefix to strip", "/api", "/api", false},
		{"a path merely starting with the letters api is untouched", "/apifoo/Bar", "/apifoo/Bar", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, had := trimAPIPrefix(tt.path)
			if got != tt.want || had != tt.wantHad {
				t.Errorf("trimAPIPrefix(%q) = (%q,%v), want (%q,%v)", tt.path, got, had, tt.want, tt.wantHad)
			}
		})
	}
}
