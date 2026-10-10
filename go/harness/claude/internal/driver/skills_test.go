package driver

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

func TestRunFailsTurnWhenSkillsAreUnavailable(t *testing.T) {
	calls := 0
	d := NewProcessDriver(ProcessConfig{
		// Never started: the turn fails before the native process runs.
		Executable: filepath.Join(t.TempDir(), "missing"), Workspace: t.TempDir(),
		EnsureSkills: func(context.Context) error {
			calls++
			return errors.New("materialize skill \"review\": exit status 128")
		},
	})
	outcome, err := d.Run(context.Background(), runtime.Turn{Prompt: "hello"}, nil)
	if err != nil {
		t.Fatalf("Run() error = %v, want a task failure", err)
	}
	if outcome.Failure == nil || outcome.Failure.Message != "skills_unavailable: materialize skill \"review\": exit status 128" {
		t.Fatalf("Run() outcome = %#v", outcome)
	}
	if calls != 1 {
		t.Fatalf("EnsureSkills called %d times", calls)
	}
}
