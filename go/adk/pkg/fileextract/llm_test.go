package fileextract

import (
	"context"
	"iter"
	"strings"
	"testing"

	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type recordingLLM struct{ got *model.LLMRequest }

func (r *recordingLLM) Name() string { return "recording" }

func (r *recordingLLM) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	r.got = req
	return func(func(*model.LLMResponse, error) bool) {}
}

func TestWithFileText(t *testing.T) {
	csv := &genai.Part{InlineData: &genai.Blob{Data: []byte("vendor,amount\nAcme,42"), MIMEType: "text/csv", DisplayName: "invoice.csv"}}
	image := &genai.Part{InlineData: &genai.Blob{Data: []byte{0x89}, MIMEType: "image/png"}}
	history := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "hi"}, csv}}
	user := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "explain"}, csv, image}}
	req := &model.LLMRequest{Contents: []*genai.Content{history, user}}

	inner := &recordingLLM{}
	for range WithFileText(inner).GenerateContent(t.Context(), req, false) {
	}

	got := inner.got.Contents
	if got[0] != history {
		t.Error("non-user content was converted or copied")
	}
	parts := got[1].Parts
	if want := "Contents of uploaded file \"invoice.csv\":\n\nvendor,amount\nAcme,42"; parts[1].Text != want || parts[1].InlineData != nil {
		t.Errorf("file part = %+v, want text %q", parts[1], want)
	}
	if parts[2] != image {
		t.Error("image part was not passed through")
	}
	if user.Parts[1] != csv || req.Contents[1] != user {
		t.Error("caller's request was mutated")
	}
}

func TestWithFileText_NoFilesPassesRequestThrough(t *testing.T) {
	dataPart := &genai.Part{InlineData: &genai.Blob{MIMEType: "text/plain", Data: []byte(`<a2a_datapart_json>{"a":1}</a2a_datapart_json>`)}}
	req := &model.LLMRequest{Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hi"}, dataPart}}}}
	inner := &recordingLLM{}
	for range WithFileText(inner).GenerateContent(t.Context(), req, false) {
	}
	if inner.got != req {
		t.Error("request without files (a data part is not one) was copied")
	}
}

func TestWithFileText_NilRequest(t *testing.T) {
	inner := &recordingLLM{got: &model.LLMRequest{}}
	for range WithFileText(inner).GenerateContent(t.Context(), nil, false) {
	}
	if inner.got != nil {
		t.Error("nil request was not passed through")
	}
}

type googleRecordingLLM struct{ recordingLLM }

func (googleRecordingLLM) GetGoogleLLMVariant() genai.Backend { return genai.BackendGeminiAPI }

func TestWithFileText_KeepsGoogleLLMVariant(t *testing.T) {
	g, ok := WithFileText(&googleRecordingLLM{}).(interface{ GetGoogleLLMVariant() genai.Backend })
	if !ok {
		t.Fatal("wrapper hides GetGoogleLLMVariant")
	}
	if got := g.GetGoogleLLMVariant(); got != genai.BackendGeminiAPI {
		t.Errorf("variant = %v, want %v", got, genai.BackendGeminiAPI)
	}
}

func TestWithFileText_LeavesDataPartsNextToFiles(t *testing.T) {
	dataPart := &genai.Part{InlineData: &genai.Blob{MIMEType: "text/plain", Data: []byte(`<a2a_datapart_json>{"a":1}</a2a_datapart_json>`)}}
	file := &genai.Part{InlineData: &genai.Blob{MIMEType: "text/plain", DisplayName: "notes.txt", Data: []byte("<a2a_datapart_json> is just text here")}}
	req := &model.LLMRequest{Contents: []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{dataPart, file}}}}
	inner := &recordingLLM{}
	for range WithFileText(inner).GenerateContent(t.Context(), req, false) {
	}
	parts := inner.got.Contents[0].Parts
	if parts[0] != dataPart {
		t.Error("data part was converted")
	}
	if !strings.Contains(parts[1].Text, "is just text here") {
		t.Errorf("file was not converted: %+v", parts[1])
	}
}

// ADK blanks DisplayName for Gemini API models before the wrapper runs; names must survive.
func TestWithFileText_RunnerKeepsNamesForGeminiAPI(t *testing.T) {
	inner := &googleRecordingLLM{}
	a, err := llmagent.New(llmagent.Config{Name: "files", Model: WithFileText(inner)})
	if err != nil {
		t.Fatal(err)
	}
	sessions := adksession.InMemoryService()
	r, err := runner.New(runner.Config{AppName: "test", Agent: a, SessionService: sessions})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(t.Context(), &adksession.CreateRequest{AppName: "test", UserID: "user"})
	if err != nil {
		t.Fatal(err)
	}
	msg := genai.NewContentFromParts([]*genai.Part{
		genai.NewPartFromText("compare these"),
		{InlineData: &genai.Blob{Data: []byte("id,v\n1,a"), MIMEType: "text/csv", DisplayName: "before.csv"}},
		{InlineData: &genai.Blob{Data: []byte("id,v\n1,b"), MIMEType: "text/csv", DisplayName: "after.csv"}},
		{InlineData: &genai.Blob{Data: []byte("remember the milk"), MIMEType: "application/octet-stream", DisplayName: "notes.txt"}},
	}, genai.RoleUser)
	for _, err := range r.Run(t.Context(), "user", sess.Session.ID(), msg, adkagent.RunConfig{}) {
		if err != nil {
			t.Fatal(err)
		}
	}

	if inner.got == nil {
		t.Fatal("model was not called")
	}
	var text strings.Builder
	for _, c := range inner.got.Contents {
		for _, p := range c.Parts {
			text.WriteString(p.Text + "\n")
		}
	}
	for _, want := range []string{`Contents of uploaded file "before.csv"`, `Contents of uploaded file "after.csv"`, "remember the milk"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("request text missing %q:\n%s", want, text.String())
		}
	}
}

func TestWithUploadName(t *testing.T) {
	uploads := []*genai.Blob{
		{Data: []byte("a"), MIMEType: "text/plain", DisplayName: "old.txt"},
		{Data: []byte("a"), MIMEType: "text/plain", DisplayName: "new.txt"},
	}
	tests := []struct {
		name string
		blob *genai.Blob
		want string
	}{
		{name: "named blob kept", blob: &genai.Blob{Data: []byte("a"), MIMEType: "text/plain", DisplayName: "mine.txt"}, want: "mine.txt"},
		{name: "blank takes latest identical upload", blob: &genai.Blob{Data: []byte("a"), MIMEType: "text/plain"}, want: "new.txt"},
		{name: "different bytes stay blank", blob: &genai.Blob{Data: []byte("b"), MIMEType: "text/plain"}, want: ""},
		{name: "different mime stays blank", blob: &genai.Blob{Data: []byte("a"), MIMEType: "text/csv"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := tt.blob.DisplayName
			if got := withUploadName(tt.blob, uploads).DisplayName; got != tt.want {
				t.Errorf("DisplayName = %q, want %q", got, tt.want)
			}
			if tt.blob.DisplayName != orig {
				t.Error("input blob was mutated")
			}
		})
	}
}
