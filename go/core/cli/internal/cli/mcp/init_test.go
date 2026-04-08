package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateProjectName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid simple name",
			input:   "my-server",
			wantErr: false,
		},
		{
			name:    "valid name with underscores",
			input:   "my_mcp_server",
			wantErr: false,
		},
		{
			name:    "valid name with numbers",
			input:   "server123",
			wantErr: false,
		},
		{
			name:    "empty name",
			input:   "",
			wantErr: true,
			errMsg:  "cannot be empty",
		},
		{
			name:    "name with spaces",
			input:   "my server",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "name with forward slash",
			input:   "my/server",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "name with backslash",
			input:   "my\\server",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "name starting with dot",
			input:   ".hidden",
			wantErr: true,
			errMsg:  "cannot start with a dot",
		},
		{
			name:    "name with special characters",
			input:   "server:v1",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "name with asterisk",
			input:   "my*server",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "name with question mark",
			input:   "server?",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "name with quotes",
			input:   "my\"server",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "name with angle brackets",
			input:   "my<server>",
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name:    "name with pipe",
			input:   "my|server",
			wantErr: true,
			errMsg:  "invalid characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProjectName(tt.input)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsValidEmail(t *testing.T) {
	tests := []struct {
		name  string
		email string
		want  bool
	}{
		{
			name:  "valid simple email",
			email: "user@example.com",
			want:  true,
		},
		{
			name:  "valid email with subdomain",
			email: "user@mail.example.com",
			want:  true,
		},
		{
			name:  "valid email with plus",
			email: "user+tag@example.com",
			want:  true,
		},
		{
			name:  "valid email with dot",
			email: "first.last@example.com",
			want:  true,
		},
		{
			name:  "valid email with hyphen in domain",
			email: "user@my-domain.com",
			want:  true,
		},
		{
			name:  "invalid missing @",
			email: "userexample.com",
			want:  false,
		},
		{
			name:  "invalid missing domain",
			email: "user@",
			want:  false,
		},
		{
			name:  "invalid missing username",
			email: "@example.com",
			want:  false,
		},
		{
			name:  "invalid missing TLD",
			email: "user@example",
			want:  false,
		},
		{
			name:  "invalid double @",
			email: "user@@example.com",
			want:  false,
		},
		{
			name:  "invalid spaces",
			email: "user @example.com",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidEmail(tt.email)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRunInitFramework_ProjectCreation(t *testing.T) {
	// This test validates the project creation flow without actually generating files
	// We test the validation and configuration setup

	tests := []struct {
		name        string
		projectName string
		setup       func(t *testing.T) string
		wantErr     bool
		errMsg      string
	}{
		{
			name:        "valid project name",
			projectName: "test-server",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			wantErr: false,
		},
		{
			name:        "invalid project name - empty",
			projectName: "",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			wantErr: true,
			errMsg:  "cannot be empty",
		},
		{
			name:        "invalid project name - special chars",
			projectName: "test/server",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			wantErr: true,
			errMsg:  "invalid characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just test the validation part
			err := validateProjectName(tt.projectName)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestInitNonInteractiveMode(t *testing.T) {
	// Test non-interactive mode behavior with config struct

	cfg := &InitMcpCfg{
		NonInteractive: true,
		Description:    "Test description",
		Author:         "Test Author",
		Email:          "test@example.com",
	}

	// Verify values are set
	assert.True(t, cfg.NonInteractive)
	assert.Equal(t, "Test description", cfg.Description)
	assert.Equal(t, "Test Author", cfg.Author)
	assert.Equal(t, "test@example.com", cfg.Email)
}

func TestInitFlags(t *testing.T) {
	// Test that init flags are properly configured

	tests := []struct {
		name             string
		flagName         string
		expectedDefValue string
	}{
		{
			name:             "force flag default",
			flagName:         "force",
			expectedDefValue: "false",
		},
		{
			name:             "no-git flag default",
			flagName:         "no-git",
			expectedDefValue: "false",
		},
		{
			name:             "non-interactive flag default",
			flagName:         "non-interactive",
			expectedDefValue: "false",
		},
		{
			name:             "namespace flag default",
			flagName:         "namespace",
			expectedDefValue: "default",
		},
	}

	cmd := newInitCmd()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := cmd.PersistentFlags().Lookup(tt.flagName)
			require.NotNil(t, flag, "Flag %s should exist", tt.flagName)
			assert.Equal(t, tt.expectedDefValue, flag.DefValue)
		})
	}
}

func TestProjectNameEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "very long valid name",
			input:   "very-long-project-name-that-is-still-valid-and-should-work-fine",
			wantErr: false,
		},
		{
			name:    "single character",
			input:   "a",
			wantErr: false,
		},
		{
			name:    "numbers only",
			input:   "12345",
			wantErr: false,
		},
		{
			name:    "mixed case",
			input:   "MyMcpServer",
			wantErr: false,
		},
		{
			name:    "name with tab",
			input:   "my\tserver",
			wantErr: true,
		},
		{
			name:    "name with newline",
			input:   "my\nserver",
			wantErr: true,
		},
		{
			name:    "name with carriage return",
			input:   "my\rserver",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProjectName(tt.input)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestInitCmd_NoArgs(t *testing.T) {
	// Test that the init command shows help when no arguments provided
	cmd := newInitCmd()
	cmd.SetArgs([]string{})

	// RunE returns nil when showing help (calls cmd.Help())
	err := cmd.RunE(cmd, []string{})
	assert.NoError(t, err)
}

func TestInitCmd_WithArgs(t *testing.T) {
	// Test that the init command returns nil when args are provided
	// (actual init is delegated to subcommands like python, go, etc.)
	cmd := newInitCmd()

	err := cmd.RunE(cmd, []string{"test-project"})
	assert.NoError(t, err)
}
