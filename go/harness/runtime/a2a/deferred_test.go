package a2a

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

type countingExecutor struct{ calls, cancels int }

func (c *countingExecutor) Execute(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	c.calls++
	return func(func(a2atype.Event, error) bool) {}
}

func (c *countingExecutor) Cancel(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	c.cancels++
	return func(func(a2atype.Event, error) bool) {}
}

type countingCloser struct{ closes int }

func (c *countingCloser) Close() error { c.closes++; return nil }

func drainEvents(seq iter.Seq2[a2atype.Event, error]) error {
	for _, err := range seq {
		if err != nil {
			return err
		}
	}
	return nil
}

func TestDeferredBuildsOnceAfterDirExists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mount")
	inner := &countingExecutor{}
	builds := 0
	deferred, err := NewDeferred(context.Background(), dir, func(context.Context) (a2asrv.AgentExecutor, io.Closer, error) {
		builds++
		return inner, io.NopCloser(nil), nil
	})
	if err != nil || deferred.Ready() {
		t.Fatalf("NewDeferred without dir: err = %v, ready = %v", err, deferred.Ready())
	}
	execCtx := &a2asrv.ExecutorContext{}

	if err := drainEvents(deferred.Execute(context.Background(), execCtx)); err == nil {
		t.Fatal("Execute succeeded before the durable dir existed")
	}
	if builds != 0 {
		t.Fatalf("build ran %d times before the dir existed", builds)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := drainEvents(deferred.Execute(context.Background(), execCtx)); err != nil {
			t.Fatalf("Execute after mount: %v", err)
		}
	}
	if builds != 1 || inner.calls != 2 {
		t.Fatalf("builds = %d, calls = %d; want 1 and 2", builds, inner.calls)
	}
}

func TestDeferredRetriesFailedBuild(t *testing.T) {
	attempts := 0
	dir := filepath.Join(t.TempDir(), "mount")
	deferred, err := NewDeferred(context.Background(), dir, func(context.Context) (a2asrv.AgentExecutor, io.Closer, error) {
		attempts++
		if attempts == 1 {
			return nil, nil, errors.New("boom")
		}
		return &countingExecutor{}, io.NopCloser(nil), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	execCtx := &a2asrv.ExecutorContext{}
	if err := drainEvents(deferred.Execute(context.Background(), execCtx)); err == nil {
		t.Fatal("first Execute should surface the build error")
	}
	if err := drainEvents(deferred.Execute(context.Background(), execCtx)); err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if attempts != 2 || !deferred.Ready() {
		t.Fatalf("attempts = %d, ready = %v; want 2 and true", attempts, deferred.Ready())
	}
}

func TestDeferredCancelAndClose(t *testing.T) {
	tests := []struct {
		name       string
		dir        func(t *testing.T) string
		buildErr   error
		wantNewErr bool
		wantCancel bool // Cancel reaches the inner executor
		wantCloses int
	}{
		{name: "cancel and close before build", dir: func(t *testing.T) string { return filepath.Join(t.TempDir(), "mount") }},
		{name: "cancel and close after eager build", dir: func(t *testing.T) string { return t.TempDir() }, wantCancel: true, wantCloses: 1},
		{name: "build reports a missing file", dir: func(t *testing.T) string { return t.TempDir() }, buildErr: fs.ErrNotExist, wantNewErr: true},
		{name: "dir is a file", dir: func(t *testing.T) string {
			f := filepath.Join(t.TempDir(), "mount")
			if err := os.WriteFile(f, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			return f
		}, wantNewErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner := &countingExecutor{}
			closer := &countingCloser{}
			deferred, err := NewDeferred(context.Background(), tt.dir(t), func(context.Context) (a2asrv.AgentExecutor, io.Closer, error) {
				if tt.buildErr != nil {
					return nil, nil, tt.buildErr
				}
				return inner, closer, nil
			})
			if (err != nil) != tt.wantNewErr {
				t.Fatalf("NewDeferred err = %v, want error %v", err, tt.wantNewErr)
			}
			if tt.wantNewErr {
				return
			}
			execCtx := &a2asrv.ExecutorContext{}
			cancelErr := drainEvents(deferred.Cancel(context.Background(), execCtx))
			if (cancelErr == nil) != tt.wantCancel || (inner.cancels == 1) != tt.wantCancel {
				t.Fatalf("Cancel err = %v, inner cancels = %d, want forwarded %v", cancelErr, inner.cancels, tt.wantCancel)
			}
			for range 2 {
				if err := deferred.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if closer.closes != tt.wantCloses {
				t.Fatalf("closes = %d, want %d", closer.closes, tt.wantCloses)
			}
		})
	}
}
