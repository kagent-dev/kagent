package a2a

import (
	"context"
	"encoding/base64"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/go-logr/logr"
	adkartifact "google.golang.org/adk/artifact"
	"google.golang.org/adk/runner"
	"google.golang.org/genai"
)

// fakeQueue is a minimal eventqueue.Queue that records written events.
type fakeQueue struct {
	events []a2atype.Event
}

func (q *fakeQueue) Write(_ context.Context, event a2atype.Event) error {
	q.events = append(q.events, event)
	return nil
}

func (q *fakeQueue) WriteVersioned(_ context.Context, event a2atype.Event, _ a2atype.TaskVersion) error {
	q.events = append(q.events, event)
	return nil
}

func (q *fakeQueue) Read(_ context.Context) (a2atype.Event, a2atype.TaskVersion, error) {
	return nil, a2atype.TaskVersionMissing, nil
}

func (q *fakeQueue) Close() error { return nil }

// ---------------------------------------------------------------------------
// maxArtifactBytes
// ---------------------------------------------------------------------------

func TestMaxArtifactBytes(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int
	}{
		{name: "default", env: "", want: defaultMaxArtifactBytes},
		{name: "override", env: "1024", want: 1024},
		{name: "invalid falls back", env: "not-a-number", want: defaultMaxArtifactBytes},
		{name: "zero falls back", env: "0", want: defaultMaxArtifactBytes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv(envMaxArtifactBytes, tt.env)
			}
			if got := MaxArtifactBytes(); got != tt.want {
				t.Errorf("MaxArtifactBytes() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// checkInboundFileSizes
// ---------------------------------------------------------------------------

func fileBytesMessage(name string, data []byte) *a2atype.Message {
	return a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.FilePart{
		File: a2atype.FileBytes{
			FileMeta: a2atype.FileMeta{Name: name, MimeType: "text/plain"},
			Bytes:    base64.StdEncoding.EncodeToString(data),
		},
	})
}

func TestCheckInboundFileSizes(t *testing.T) {
	tests := []struct {
		name    string
		msg     *a2atype.Message
		limit   int
		wantErr bool
	}{
		{name: "nil message", msg: nil, limit: 10, wantErr: false},
		{name: "under limit", msg: fileBytesMessage("a.txt", []byte("hello")), limit: 10, wantErr: false},
		{name: "at limit", msg: fileBytesMessage("a.txt", []byte("12345")), limit: 5, wantErr: false},
		{name: "over limit", msg: fileBytesMessage("a.txt", []byte("123456")), limit: 5, wantErr: true},
		{name: "text only ignored", msg: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.TextPart{Text: "hi"}), limit: 1, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkInboundFileSizes(tt.msg, tt.limit)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkInboundFileSizes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBase64DecodedLen(t *testing.T) {
	// Covers each padding case so the allocation-free size check stays exact
	// for clean StdEncoding input (the format the UI emits).
	for _, n := range []int{0, 1, 2, 3, 5, 10, 64, 1023, 1 << 20} {
		data := make([]byte, n)
		encoded := base64.StdEncoding.EncodeToString(data)
		if got := base64DecodedLen(encoded); got != n {
			t.Errorf("base64DecodedLen(%d-byte payload) = %d, want %d", n, got, n)
		}
	}
}

func TestCheckInboundFileSizes_PointerPart(t *testing.T) {
	msg := a2atype.NewMessage(a2atype.MessageRoleUser, &a2atype.FilePart{
		File: a2atype.FileBytes{
			FileMeta: a2atype.FileMeta{Name: "big.bin"},
			Bytes:    base64.StdEncoding.EncodeToString([]byte("0123456789")),
		},
	})
	if err := checkInboundFileSizes(msg, 5); err == nil {
		t.Error("expected error for oversized pointer FilePart, got nil")
	}
}

// ---------------------------------------------------------------------------
// emitArtifacts
// ---------------------------------------------------------------------------

func TestEmitArtifacts_EmitsFilePart(t *testing.T) {
	ctx := context.Background()
	const (
		appName   = "test-app"
		userID    = "user-1"
		sessionID = "session-1"
		fileName  = "report.csv"
		mimeType  = "text/csv"
	)
	wantBytes := []byte("a,b,c\n1,2,3\n")

	svc := adkartifact.InMemoryService()
	saveResp, err := svc.Save(ctx, &adkartifact.SaveRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
		FileName:  fileName,
		Part:      &genai.Part{InlineData: &genai.Blob{Data: wantBytes, MIMEType: mimeType}},
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	e := &KAgentExecutor{
		runnerConfig: runner.Config{ArtifactService: svc},
		appName:      appName,
		logger:       logr.Discard(),
	}

	reqCtx := &a2asrv.RequestContext{
		TaskID:    a2atype.NewTaskID(),
		ContextID: sessionID,
	}
	queue := &fakeQueue{}

	e.emitArtifacts(ctx, reqCtx, queue, userID, sessionID,
		map[string]int64{fileName: saveResp.Version}, map[string]any{})

	if len(queue.events) != 1 {
		t.Fatalf("expected 1 artifact event, got %d", len(queue.events))
	}
	artifactEvent, ok := queue.events[0].(*a2atype.TaskArtifactUpdateEvent)
	if !ok {
		t.Fatalf("event type = %T, want *a2atype.TaskArtifactUpdateEvent", queue.events[0])
	}
	if !artifactEvent.LastChunk {
		t.Error("expected LastChunk = true")
	}
	if len(artifactEvent.Artifact.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(artifactEvent.Artifact.Parts))
	}
	fp, ok := artifactEvent.Artifact.Parts[0].(a2atype.FilePart)
	if !ok {
		t.Fatalf("part type = %T, want a2atype.FilePart", artifactEvent.Artifact.Parts[0])
	}
	fb, ok := fp.File.(a2atype.FileBytes)
	if !ok {
		t.Fatalf("file type = %T, want a2atype.FileBytes", fp.File)
	}
	if fb.Name != fileName {
		t.Errorf("file name = %q, want %q", fb.Name, fileName)
	}
	if fb.MimeType != mimeType {
		t.Errorf("mime type = %q, want %q", fb.MimeType, mimeType)
	}
	gotBytes, err := base64.StdEncoding.DecodeString(fb.Bytes)
	if err != nil {
		t.Fatalf("decode bytes: %v", err)
	}
	if string(gotBytes) != string(wantBytes) {
		t.Errorf("bytes = %q, want %q", gotBytes, wantBytes)
	}
}

func TestEmitArtifacts_SkipsMissingArtifact(t *testing.T) {
	ctx := context.Background()
	svc := adkartifact.InMemoryService()
	e := &KAgentExecutor{
		runnerConfig: runner.Config{ArtifactService: svc},
		appName:      "test-app",
		logger:       logr.Discard(),
	}
	reqCtx := &a2asrv.RequestContext{TaskID: a2atype.NewTaskID(), ContextID: "s1"}
	queue := &fakeQueue{}

	// Reference an artifact that was never saved → load fails → skip, no panic.
	e.emitArtifacts(ctx, reqCtx, queue, "user-1", "s1",
		map[string]int64{"missing.txt": 1}, map[string]any{})

	if len(queue.events) != 0 {
		t.Errorf("expected 0 events for missing artifact, got %d", len(queue.events))
	}
}
