package a2a

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"sync"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// Deferred wraps an executor whose build may have to wait for dir to exist.
// Some hosts attach the durable directory only when the first request arrives.
type Deferred struct {
	dir   string
	build func(context.Context) (a2asrv.AgentExecutor, io.Closer, error)

	mu     sync.Mutex
	inner  a2asrv.AgentExecutor
	closer io.Closer
}

var (
	_ a2asrv.AgentExecutor = (*Deferred)(nil)
	_ io.Closer            = (*Deferred)(nil)
)

// NewDeferred builds immediately when dir exists. Otherwise build runs on the
// first Execute after dir appears, retrying on failure until it succeeds.
func NewDeferred(ctx context.Context, dir string, build func(context.Context) (a2asrv.AgentExecutor, io.Closer, error)) (*Deferred, error) {
	d := &Deferred{dir: dir, build: build}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return d, nil
	}
	if _, err := d.ensure(ctx); err != nil {
		return nil, err
	}
	return d, nil
}

// Ready reports whether the real executor has been built.
func (d *Deferred) Ready() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inner != nil
}

func (d *Deferred) ensure(ctx context.Context) (a2asrv.AgentExecutor, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.inner != nil {
		return d.inner, nil
	}
	// Gate on the directory: build would MkdirAll onto the container filesystem
	// and the mount would later shadow those files.
	info, err := os.Stat(d.dir)
	if err != nil {
		return nil, fmt.Errorf("durable directory %s is not available: %w", d.dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("durable directory %s is not a directory", d.dir)
	}
	inner, closer, err := d.build(ctx)
	if err != nil {
		if closer != nil {
			_ = closer.Close()
		}
		return nil, err
	}
	d.inner, d.closer = inner, closer
	return inner, nil
}

func (d *Deferred) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	inner, err := d.ensure(ctx)
	if err != nil {
		return func(yield func(a2atype.Event, error) bool) { yield(nil, err) }
	}
	return inner.Execute(ctx, execCtx)
}

func (d *Deferred) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	d.mu.Lock()
	inner := d.inner
	d.mu.Unlock()
	if inner == nil {
		return func(yield func(a2atype.Event, error) bool) { yield(nil, fmt.Errorf("no active task")) }
	}
	return inner.Cancel(ctx, execCtx)
}

func (d *Deferred) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	closer := d.closer
	d.closer = nil
	if closer != nil {
		return closer.Close()
	}
	return nil
}
