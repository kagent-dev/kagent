package executor

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/harness/codex/config"
)

func TestNewRejectsBadInput(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{name: "relative data dir", cfg: Config{ConfigJSON: fmt.Appendf(nil, `{"version":%d,"codex_executable":"codex","model":"m","provider":{"name":"openai"},"max_frame_bytes":100,"max_stderr_bytes":100,"interrupt_grace_millis":100}`, config.Version), DataDir: "relative/dir"}, wantErr: "absolute path"},
		{name: "malformed config", cfg: Config{ConfigJSON: []byte(`{"version":`), DataDir: t.TempDir()}, wantErr: "decode config"},
		{name: "unknown config field", cfg: Config{ConfigJSON: []byte(`{"nope":1}`), DataDir: t.TempDir()}, wantErr: "decode config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(context.Background(), tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("New() err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
