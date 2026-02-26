package cli

import (
	"testing"
)

func TestValidateAgentName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid simple name", input: "dice", wantErr: false},
		{name: "valid with underscore", input: "my_agent", wantErr: false},
		{name: "valid with digits", input: "agent1", wantErr: false},
		{name: "valid starts with underscore", input: "_private", wantErr: false},
		{name: "valid mixed", input: "My_Agent_2", wantErr: false},
		{name: "invalid dash", input: "hello-agent", wantErr: true},
		{name: "invalid starts with digit", input: "1agent", wantErr: true},
		{name: "invalid space", input: "my agent", wantErr: true},
		{name: "invalid dot", input: "my.agent", wantErr: true},
		{name: "empty", input: "", wantErr: true},
		{name: "invalid starts with dash", input: "-agent", wantErr: true},
		{name: "single letter", input: "a", wantErr: false},
		{name: "single underscore", input: "_", wantErr: false},
		{name: "valid unicode letter start", input: "α_agent", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAgentName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAgentName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
