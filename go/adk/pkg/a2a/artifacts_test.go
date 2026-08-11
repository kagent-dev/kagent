package a2a

import (
	"context"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/go-logr/logr"
	adkartifact "google.golang.org/adk/v2/artifact"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestMaxArtifactBytes(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int
	}{
		{name: "default", want: defaultMaxArtifactBytes},
		{name: "override", env: "1024", want: 1024},
		{name: "invalid falls back", env: "invalid", want: defaultMaxArtifactBytes},
		{name: "zero falls back", env: "0", want: defaultMaxArtifactBytes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envMaxArtifactBytes, tt.env)
			if got := MaxArtifactBytes(); got != tt.want {
				t.Errorf("MaxArtifactBytes() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCheckInboundFileSizes(t *testing.T) {
	fileMessage := func(data []byte) *a2atype.Message {
		part := a2atype.NewRawPart(data)
		part.Filename = "upload.txt"
		return a2atype.NewMessage(a2atype.MessageRoleUser, part)
	}
	tests := []struct {
		name    string
		message *a2atype.Message
		limit   int
		wantErr bool
	}{
		{name: "nil message", limit: 5},
		{name: "under limit", message: fileMessage([]byte("1234")), limit: 5},
		{name: "at limit", message: fileMessage([]byte("12345")), limit: 5},
		{name: "over limit", message: fileMessage([]byte("123456")), limit: 5, wantErr: true},
		{name: "text ignored", message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("long text")), limit: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkInboundFileSizes(tt.message, tt.limit)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkInboundFileSizes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAppendSavedArtifacts(t *testing.T) {
	ctx := context.Background()
	service := adkartifact.InMemoryService()
	response, err := service.Save(ctx, &adkartifact.SaveRequest{
		AppName: "app", UserID: "user", SessionID: "session", FileName: "report.csv",
		Part: &genai.Part{InlineData: &genai.Blob{Data: []byte("a,b\n1,2\n"), MIMEType: "text/csv"}},
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	event := &adksession.Event{
		Actions: adksession.EventActions{ArtifactDelta: map[string]int64{"report.csv": response.Version}},
	}
	update := &a2atype.TaskArtifactUpdateEvent{Artifact: &a2atype.Artifact{}}
	appendSavedArtifacts(ctx, service, "app", "user", "session", event, update, logr.Discard())

	if len(update.Artifact.Parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(update.Artifact.Parts))
	}
	part := update.Artifact.Parts[0]
	if part.Filename != "report.csv" || part.MediaType != "text/csv" {
		t.Errorf("part = (%q, %q), want report.csv, text/csv", part.Filename, part.MediaType)
	}
	if string(part.Raw()) != "a,b\n1,2\n" {
		t.Errorf("raw data = %q", part.Raw())
	}
}
