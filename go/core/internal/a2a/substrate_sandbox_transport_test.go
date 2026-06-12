package a2a

import (
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSuspendSessionActorOnClose(t *testing.T) {
	t.Parallel()

	var suspended atomic.Bool
	body := &trackingReadCloser{data: []byte("ok")}

	wrapped := &suspendSessionActorOnClose{
		ReadCloser: body,
		suspend: func() {
			suspended.Store(true)
		},
	}

	if _, err := io.ReadAll(wrapped); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if suspended.Load() {
		t.Fatal("suspend should not run before Close")
	}
	if err := wrapped.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !suspended.Load() {
		t.Fatal("expected suspend after Close")
	}
	if body.closed != 1 {
		t.Fatalf("expected underlying body closed once, got %d", body.closed)
	}
}

type trackingReadCloser struct {
	data   []byte
	offset int
	closed int
}

func (t *trackingReadCloser) Read(p []byte) (int, error) {
	if t.offset >= len(t.data) {
		return 0, io.EOF
	}
	n := copy(p, t.data[t.offset:])
	t.offset += n
	return n, nil
}

func (t *trackingReadCloser) Close() error {
	t.closed++
	return nil
}

func TestExtractA2AContextID(t *testing.T) {
	t.Parallel()

	got, err := extractA2AContextID([]byte(`{"params":{"message":{"contextId":"sess-1"}}}`))
	if err != nil {
		t.Fatalf("extractA2AContextID: %v", err)
	}
	if got != "sess-1" {
		t.Fatalf("got %q, want sess-1", got)
	}
}

func TestSuspendSessionActorOnClosePreservesBody(t *testing.T) {
	t.Parallel()

	rc := &suspendSessionActorOnClose{
		ReadCloser: io.NopCloser(strings.NewReader("payload")),
		suspend:    func() {},
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("got %q", got)
	}
}
