package driver

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

// bearer is an unsigned JWT with the given claims: the harness reads exp to
// bound a turn and never verifies the token, which the MCP server does.
func bearer(claims string) string {
	encode := base64.RawURLEncoding.EncodeToString
	return "Bearer " + encode([]byte(`{"alg":"RS256"}`)) + "." + encode([]byte(claims)) + ".signature"
}

func TestBearerExpiry(t *testing.T) {
	for _, test := range []struct {
		name       string
		credential string
		want       time.Time
		ok         bool
	}{
		{name: "JWT bearer", credential: bearer(`{"sub":"person","exp":1790000000}`), want: time.Unix(1790000000, 0), ok: true},
		{name: "scheme in another case", credential: strings.Replace(bearer(`{"exp":1790000000}`), "Bearer", "bearer", 1), want: time.Unix(1790000000, 0), ok: true},
		{name: "fractional exp", credential: bearer(`{"exp":1790000000.5}`), want: time.Unix(1790000000, 0), ok: true},
		{name: "no exp", credential: bearer(`{"sub":"person"}`)},
		{name: "opaque token", credential: "Bearer opaque-token"},
		{name: "another scheme", credential: "Basic dXNlcjpwYXNz"},
		{name: "payload not JSON", credential: "Bearer a." + base64.RawURLEncoding.EncodeToString([]byte("nope")) + ".c"},
		{name: "none", credential: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := bearerExpiry(test.credential)
			if ok != test.ok || !got.Equal(test.want) {
				t.Fatalf("bearerExpiry() = %v, %v; want %v, %v", got, ok, test.want, test.ok)
			}
		})
	}
}

func TestBoundTurn(t *testing.T) {
	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	expiry := start.Add(10 * time.Minute)
	if _, ok := boundTurn(start, 0, time.Time{}); ok {
		t.Fatal("a turn with neither a time limit nor a bearer expiry must be unbounded")
	}
	bound, ok := boundTurn(start, 30*time.Minute, time.Time{})
	if !ok || !bound.at.Equal(start.Add(30*time.Minute)) || bound.stop.Reason != StopReasonTurnTimeLimit {
		t.Fatalf("time limit bound = %#v, %v", bound, ok)
	}
	if !strings.Contains(bound.stop.Message, "30m0s") || !strings.Contains(bound.stop.Message, "continue") {
		t.Fatalf("time limit message = %q, want the limit and how to continue", bound.stop.Message)
	}
	bound, ok = boundTurn(start, 30*time.Minute, expiry)
	if !ok || !bound.at.Equal(expiry.Add(-callerCredentialExpiryMargin)) || bound.stop.Reason != StopReasonCallerCredentialExpiry {
		t.Fatalf("the earlier bearer expiry must win: bound = %#v, %v", bound, ok)
	}
	if !strings.Contains(bound.stop.Message, "sign-in") || !strings.Contains(bound.stop.Message, "continue") {
		t.Fatalf("expiry message = %q, want the sign-in and how to continue", bound.stop.Message)
	}
	bound, ok = boundTurn(start, 5*time.Minute, expiry)
	if !ok || bound.stop.Reason != StopReasonTurnTimeLimit {
		t.Fatalf("the earlier time limit must win: bound = %#v, %v", bound, ok)
	}
	bound, ok = boundTurn(start, 0, expiry)
	if !ok || bound.stop.Reason != StopReasonCallerCredentialExpiry {
		t.Fatalf("a bearer expiry alone must bound the turn: bound = %#v, %v", bound, ok)
	}
}

// longTurn is a Claude that starts its session and then works until it is
// stopped, as a turn that outlives its bound does.
func longTurn(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	executable := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\nwhile :; do sleep 0.01; done\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir, executable
}

func TestProcessDriverEndsATurnAtItsTimeLimit(t *testing.T) {
	dir, executable := longTurn(t)
	d := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir, MaxEventBytes: 4096, MaxStderrBytes: 1024,
		InterruptGrace: 50 * time.Millisecond, TurnTimeout: 200 * time.Millisecond,
	})
	sink := &recordingSink{}
	started := time.Now()
	outcome, err := d.Run(t.Context(), runtime.Turn{Prompt: "hello"}, sink)
	if err != nil {
		t.Fatalf("Run() error = %v, want the cap result", err)
	}
	if outcome.Failure != nil || outcome.Pending != nil || outcome.Stop == nil || outcome.Stop.Reason != StopReasonTurnTimeLimit {
		t.Fatalf("Run() = %#v, want a completed turn stopped at its time limit", outcome)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the capped turn took %s", elapsed)
	}
	if len(sink.sessions) != 1 {
		t.Fatalf("sessions = %#v, want the session the person continues", sink.sessions)
	}
}

// TestProcessDriverEndsATurnBeforeItsBearerExpires is the acceptance case: a
// turn outliving the caller's bearer ends with the cap result before the MCP
// server could answer 401, and the credential is dropped with it.
func TestProcessDriverEndsATurnBeforeItsBearerExpires(t *testing.T) {
	dir, executable := longTurn(t)
	binder := &recordingBinder{}
	d := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir, MaxEventBytes: 4096, MaxStderrBytes: 1024,
		InterruptGrace: 50 * time.Millisecond, TurnTimeout: time.Hour, CallerCredentials: binder,
	})
	expiry := time.Now().Add(callerCredentialExpiryMargin+300*time.Millisecond).Unix() + 1
	credential := bearer(`{"sub":"person","exp":` + itoa(expiry) + `}`)
	outcome, err := d.Run(callerContext(t, credential), runtime.Turn{Prompt: "hello"}, &recordingSink{})
	if err != nil {
		t.Fatalf("Run() error = %v, want the cap result", err)
	}
	if outcome.Failure != nil || outcome.Stop == nil || outcome.Stop.Reason != StopReasonCallerCredentialExpiry {
		t.Fatalf("Run() = %#v, want a completed turn stopped at the bearer's expiry", outcome)
	}
	if time.Now().After(time.Unix(expiry, 0)) {
		t.Fatal("the turn outlived the caller's bearer")
	}
	if got := strings.Join(binder.events, ","); got != "bind:"+credential+",clear" {
		t.Fatalf("binder events = %q, want the credential dropped with the turn", got)
	}
}

// Claude Code reports its result while a backgrounded tool still runs and
// keeps the process alive until it finishes: the bound stops that too, and the
// turn keeps the result and its usage.
func TestProcessDriverBoundsWorkAfterTheResult(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}' '{\"type\":\"result\",\"subtype\":\"success\",\"session_id\":\"11111111-1111-4111-8111-111111111111\",\"num_turns\":2,\"total_cost_usd\":0.5,\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}'\nwhile :; do sleep 0.01; done\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	d := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir, MaxEventBytes: 4096, MaxStderrBytes: 1024,
		InterruptGrace: 50 * time.Millisecond, TurnTimeout: 200 * time.Millisecond,
	})
	started := time.Now()
	outcome, err := d.Run(t.Context(), runtime.Turn{Prompt: "hello"}, &recordingSink{})
	if err != nil {
		t.Fatalf("Run() error = %v, want the cap result", err)
	}
	if outcome.Failure != nil || outcome.Stop == nil || outcome.Stop.Reason != StopReasonTurnTimeLimit {
		t.Fatalf("Run() = %#v, want a completed turn stopped at its time limit", outcome)
	}
	if outcome.Usage == nil || outcome.Usage.NumTurns != 2 {
		t.Fatalf("usage = %#v, want the result's usage kept", outcome.Usage)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the capped turn took %s", elapsed)
	}
}

func TestProcessDriverDoesNotStartATurnWithAnExpiredBearer(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	executable := filepath.Join(dir, "claude")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\ntouch \""+started+"\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	d := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir, MaxEventBytes: 4096, MaxStderrBytes: 1024,
		InterruptGrace: 50 * time.Millisecond, CallerCredentials: &recordingBinder{},
	})
	credential := bearer(`{"exp":` + itoa(time.Now().Add(-time.Minute).Unix()) + `}`)
	outcome, err := d.Run(callerContext(t, credential), runtime.Turn{Prompt: "hello"}, &recordingSink{})
	if err != nil || outcome.Stop == nil || outcome.Stop.Reason != StopReasonCallerCredentialExpiry {
		t.Fatalf("Run() = %#v, %v; want the cap result", outcome, err)
	}
	if _, err := os.Stat(started); err == nil {
		t.Fatal("Claude started on a bearer that had already expired")
	}
}

// A turn whose servers all receive the Actor's JWT forwards no bearer, so the
// caller's bearer bounds nothing: Claude starts on an expired one.
func TestProcessDriverIgnoresTheBearerOfAnActorJWTTurn(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	executable := filepath.Join(dir, "claude")
	script := "#!/bin/sh\ntouch \"" + started + "\"\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}' '{\"type\":\"result\",\"subtype\":\"success\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\ncat >/dev/null\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	d := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir, MaxEventBytes: 4096, MaxStderrBytes: 1024,
		InterruptGrace: 50 * time.Millisecond, CallerCredentials: &recordingBinder{actorJWTHost: "muster.example.svc"},
	})
	credential := bearer(`{"exp":` + itoa(time.Now().Add(-time.Minute).Unix()) + `}`)
	outcome, err := d.Run(callerContext(t, credential, "muster.example.svc"), runtime.Turn{Prompt: "hello"}, &recordingSink{})
	if err != nil || outcome.Stop != nil {
		t.Fatalf("Run() = %#v, %v; want a turn the bearer does not bound", outcome, err)
	}
	if _, err := os.Stat(started); err != nil {
		t.Fatal("Claude did not start: the bearer it does not forward bounded the turn")
	}
}

// Without a caller credential to forward, the bearer bounds nothing: an
// expired token on the call is not the harness's to judge.
func TestProcessDriverIgnoresTheBearerItDoesNotForward(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}' '{\"type\":\"result\",\"subtype\":\"success\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\ncat >/dev/null\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	d := NewProcessDriver(ProcessConfig{Executable: executable, Workspace: dir, MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 50 * time.Millisecond})
	credential := bearer(`{"exp":` + itoa(time.Now().Add(-time.Minute).Unix()) + `}`)
	outcome, err := d.Run(callerContext(t, credential), runtime.Turn{Prompt: "hello"}, &recordingSink{})
	if err != nil || outcome.Stop != nil || outcome.Failure != nil {
		t.Fatalf("Run() = %#v, %v; want an ordinary completed turn", outcome, err)
	}
}

func itoa(value int64) string { return strconv.FormatInt(value, 10) }
