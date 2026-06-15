package openshell_test

import (
	"testing"

	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openclaw"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/hermes"
)

func TestResolveSSHRemoteCommand(t *testing.T) {
	tests := []struct {
		name           string
		plainShell     bool
		launchOverride string
		harnessBackend string
		wantPlain      bool
		wantCmd        string
	}{
		{
			name:       "plain shell",
			plainShell: true,
			wantPlain:  true,
		},
		{
			name:           "client override",
			launchOverride: "  custom  ",
			wantPlain:      false,
			wantCmd:        "custom",
		},
		{
			name:      "unknown backend defaults to openclaw tui",
			wantPlain: false,
			wantCmd:   openclaw.DefaultSSHLaunchCommand(),
		},
		{
			name:           "hermes backend",
			harnessBackend: "hermes",
			wantPlain:      false,
			wantCmd:        hermes.DefaultSSHLaunchCommand(),
		},
		{
			name:           "openclaw backend",
			harnessBackend: "openclaw",
			wantPlain:      false,
			wantCmd:        openclaw.DefaultSSHLaunchCommand(),
		},
		{
			name:           "nemoclaw backend",
			harnessBackend: "nemoclaw",
			wantPlain:      false,
			wantCmd:        openclaw.DefaultSSHLaunchCommand(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plain, cmd := openshell.ResolveSSHRemoteCommand(tt.plainShell, tt.launchOverride, tt.harnessBackend)
			if plain != tt.wantPlain || cmd != tt.wantCmd {
				t.Fatalf("ResolveSSHRemoteCommand() = (%v, %q), want (%v, %q)", plain, cmd, tt.wantPlain, tt.wantCmd)
			}
		})
	}
}
