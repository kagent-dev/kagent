package acpshim

import (
	"testing"
	"time"
)

// TestTerminateReturnsWhenOutNotDrained guards against a teardown deadlock:
// when nobody drains c.out, the stdout reader goroutine blocks
// sending on the full channel and can never reach cmd.Wait()/close(done).
// terminate() must still reap the child and return promptly instead of
// blocking forever on <-c.done.
func TestTerminateReturnsWhenOutNotDrained(t *testing.T) {
	// Emit far more than cap(out) lines, then stay alive so terminate()
	// actually has to signal the process.
	cfg := &Config{
		ChildArgv: []string{"sh", "-c", "i=0; while [ $i -lt 300 ]; do echo line$i; i=$((i+1)); done; sleep 3600"},
	}
	c, err := startChild(cfg)
	if err != nil {
		t.Fatalf("startChild: %v", err)
	}

	// Wait until the reader goroutine has filled `out` and is blocked on its
	// next send. We deliberately never drain `out`.
	if !waitFor(3*time.Second, func() bool { return len(c.out) == cap(c.out) }) {
		t.Fatalf("out never filled (len=%d cap=%d) — test setup wrong", len(c.out), cap(c.out))
	}
	time.Sleep(200 * time.Millisecond) // let the goroutine block on the next send

	returned := make(chan struct{})
	go func() {
		c.terminate(500 * time.Millisecond)
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(4 * time.Second):
		t.Fatalf("terminate() did not return within 4s: reader goroutine is blocked " +
			"sending to the full out channel, so cmd.Wait()/close(done) never run")
	}
}

func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}
