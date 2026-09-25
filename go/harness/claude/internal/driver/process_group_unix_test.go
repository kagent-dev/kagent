//go:build unix

package driver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

func TestProcessDriverCancellationTerminatesDescendants(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "claude")
	childActivityPath := filepath.Join(dir, "child-activity")
	script := `#!/bin/sh
trap 'exit 0' INT
sh -c 'trap "" INT; while :; do printf x >> "$CHILD_ACTIVITY_PATH"; sleep 0.02; done' &
printf '%s\n' '{"type":"system","subtype":"init","session_id":"11111111-1111-4111-8111-111111111111"}'
sleep 60
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	d := NewProcessDriver(ProcessConfig{
		Executable:     executable,
		Workspace:      dir,
		Environment:    []string{"CHILD_ACTIVITY_PATH=" + childActivityPath},
		MaxEventBytes:  4096,
		MaxStderrBytes: 1024,
		InterruptGrace: 100 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	result := make(chan error, 1)
	go func() {
		_, err := d.Run(ctx, runtime.Turn{Prompt: "hello"}, &recordingSink{})
		result <- err
	}()

	waitForFile(t, childActivityPath)
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
	before, err := os.Stat(childActivityPath)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	after, err := os.Stat(childActivityPath)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() {
		t.Fatalf("descendant continued running after cancellation: activity grew from %d to %d bytes", before.Size(), after.Size())
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info, err := os.Stat(path)
		if err == nil {
			if info.Size() > 0 {
				return
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat child activity: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for child activity")
}
