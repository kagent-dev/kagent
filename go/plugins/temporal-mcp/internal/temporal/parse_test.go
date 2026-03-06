package temporal

import "testing"

func TestParseWorkflowID(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		wantAgent string
		wantSess  string
	}{
		{
			name:      "standard pattern",
			id:        "agent-k8s-agent-abc123",
			wantAgent: "k8s-agent",
			wantSess:  "abc123",
		},
		{
			name:      "simple agent name",
			id:        "agent-myagent-sess1",
			wantAgent: "myagent",
			wantSess:  "sess1",
		},
		{
			name:      "no prefix",
			id:        "workflow-123",
			wantAgent: "",
			wantSess:  "",
		},
		{
			name:      "agent prefix only",
			id:        "agent-onlyname",
			wantAgent: "onlyname",
			wantSess:  "",
		},
		{
			name:      "empty string",
			id:        "",
			wantAgent: "",
			wantSess:  "",
		},
		{
			name:      "multi-hyphen agent name",
			id:        "agent-my-k8s-agent-session42",
			wantAgent: "my-k8s-agent",
			wantSess:  "session42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent, sess := ParseWorkflowID(tt.id)
			if agent != tt.wantAgent {
				t.Errorf("agent = %q, want %q", agent, tt.wantAgent)
			}
			if sess != tt.wantSess {
				t.Errorf("session = %q, want %q", sess, tt.wantSess)
			}
		})
	}
}
