package a2a

import (
	"context"
	"iter"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/fileextract"
	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/server/adka2a/v2"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func convDataPart(data map[string]any, metadata map[string]any) *a2atype.Part {
	p := a2atype.NewDataPart(data)
	if metadata != nil {
		p.Metadata = metadata
	}
	return p
}

// ---------------------------------------------------------------------------
// convertDataPartToGenAI
// ---------------------------------------------------------------------------

func TestConvertDataPartToGenAI_FunctionCall_PublicContract(t *testing.T) {
	data := map[string]any{
		"name": "my_func",
		"args": map[string]any{"key": "value"},
		"id":   "call_1",
	}
	meta := map[string]any{
		kagenta2a.PartTypeMetadataKey: A2ADataPartMetadataTypeFunctionCall,
	}

	part, err := convertDataPartToGenAI(data, meta, kagenta2a.PartTypeMetadataKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part.FunctionCall == nil {
		t.Fatal("expected FunctionCall to be set")
	}
	if part.FunctionCall.Name != "my_func" {
		t.Errorf("name = %q, want %q", part.FunctionCall.Name, "my_func")
	}
	if part.FunctionCall.ID != "call_1" {
		t.Errorf("id = %q, want %q", part.FunctionCall.ID, "call_1")
	}
}

func TestConvertDataPartToGenAI_FunctionCall_AdkPrefix(t *testing.T) {
	data := map[string]any{
		"name": "my_func",
		"args": map[string]any{"key": "value"},
		"id":   "call_1",
	}
	meta := map[string]any{
		adka2a.ToA2AMetaKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeFunctionCall,
	}

	part, err := convertDataPartToGenAI(data, meta, adka2a.ToA2AMetaKey(A2ADataPartMetadataTypeKey))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part.FunctionCall == nil {
		t.Fatal("expected FunctionCall to be set")
	}
	if part.FunctionCall.Name != "my_func" {
		t.Errorf("name = %q, want %q", part.FunctionCall.Name, "my_func")
	}
}

func TestConvertDataPartToGenAI_FunctionResponse(t *testing.T) {
	data := map[string]any{
		"name":     "my_func",
		"response": map[string]any{"result": "ok"},
		"id":       "call_2",
	}
	meta := map[string]any{
		kagenta2a.PartTypeMetadataKey: A2ADataPartMetadataTypeFunctionResponse,
	}

	part, err := convertDataPartToGenAI(data, meta, kagenta2a.PartTypeMetadataKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part.FunctionResponse == nil {
		t.Fatal("expected FunctionResponse to be set")
	}
	if part.FunctionResponse.Name != "my_func" {
		t.Errorf("name = %q, want %q", part.FunctionResponse.Name, "my_func")
	}
	if part.FunctionResponse.ID != "call_2" {
		t.Errorf("id = %q, want %q", part.FunctionResponse.ID, "call_2")
	}
}

func TestConvertDataPartToGenAI_Nil(t *testing.T) {
	part, err := convertDataPartToGenAI(nil, nil, kagenta2a.PartTypeMetadataKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part != nil {
		t.Fatalf("expected nil part, got %v", part)
	}
}

func TestConvertDataPartToGenAI_UnknownType(t *testing.T) {
	part, err := convertDataPartToGenAI(
		map[string]any{"foo": "bar"},
		map[string]any{kagenta2a.PartTypeMetadataKey: "unknown_type"},
		kagenta2a.PartTypeMetadataKey,
	)
	if err != nil {
		t.Fatalf("unexpected error for unknown part type: %v", err)
	}
	if part == nil {
		t.Fatal("expected fallback GenAI part for unknown type")
	}
}

// ---------------------------------------------------------------------------
// a2aPartConverter
// ---------------------------------------------------------------------------

func TestA2APartConverter_TextPart(t *testing.T) {
	part, err := a2aPartConverter(context.Background(), nil, a2atype.NewTextPart("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part == nil || part.Text != "hello" {
		t.Fatalf("converted part = %#v, want text hello", part)
	}
}

func TestA2APartConverter_FileNameMetadata(t *testing.T) {
	tests := []struct {
		name string
		part *a2atype.Part
		want any
	}{
		{name: "text file keeps its name", part: fileA2APart("notes.txt", "text/plain", "hi"), want: "notes.txt"},
		{name: "image does not", part: fileA2APart("photo.png", "image/png", "\x89PNG"), want: nil},
		{name: "unnamed file does not", part: fileA2APart("", "text/plain", "hi"), want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			part, err := a2aPartConverter(context.Background(), nil, tt.part)
			if err != nil {
				t.Fatal(err)
			}
			if got := part.PartMetadata[fileextract.FilenameMetadataKey]; got != tt.want {
				t.Errorf("filename metadata = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKeepFileName_MergesMetadata(t *testing.T) {
	p := &genai.Part{InlineData: &genai.Blob{MIMEType: "text/csv", DisplayName: "a.csv"}, PartMetadata: map[string]any{"other": 1}}
	keepFileName(p)
	if p.PartMetadata["other"] != 1 || p.PartMetadata[fileextract.FilenameMetadataKey] != "a.csv" {
		t.Errorf("PartMetadata = %v, want other kept and filename added", p.PartMetadata)
	}
}

func TestA2APartConverter_DropsUnrecognisedDataPart(t *testing.T) {
	// A DataPart with no recognised part-type metadata (e.g. a HITL decision
	// payload like {decision_type: "approve"}) should be dropped silently.
	part, err := a2aPartConverter(
		context.Background(), nil,
		convDataPart(map[string]any{"decision_type": "approve"}, nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part != nil {
		t.Fatalf("converted part = %#v, want nil", part)
	}
}

func TestA2APartConverter_PublicFunctionResponse(t *testing.T) {
	// A typed function response should be converted to GenAI.
	dp := convDataPart(map[string]any{
		"name":     "my_func",
		"id":       "call_1",
		"response": map[string]any{"result": "ok"},
	}, map[string]any{
		kagenta2a.PartTypeMetadataKey: A2ADataPartMetadataTypeFunctionResponse,
	})
	part, err := a2aPartConverter(context.Background(), nil, dp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part == nil || part.FunctionResponse == nil {
		t.Fatal("expected FunctionResponse, got nil")
	}
	if part.FunctionResponse.Name != "my_func" {
		t.Errorf("name = %q, want my_func", part.FunctionResponse.Name)
	}
}

func TestGenAIPartConverter_PreservesLongRunningMetadata(t *testing.T) {
	call := genai.NewPartFromFunctionCall("dangerous_tool", map[string]any{"path": "/tmp/x"})
	call.FunctionCall.ID = "call-1"
	part, err := genAIPartConverter(
		context.Background(),
		&adksession.Event{LongRunningToolIDs: []string{"call-1"}},
		call,
	)
	if err != nil {
		t.Fatalf("genAIPartConverter() error = %v", err)
	}
	if part == nil {
		t.Fatal("genAIPartConverter() returned nil")
	}
	if got := part.Metadata[adka2a.ToA2AMetaKey(A2ADataPartMetadataIsLongRunningKey)]; got != true {
		t.Fatalf("long-running metadata = %#v, want true", got)
	}
}

// geminiAPIRecorder is a Gemini API model, so ADK blanks request DisplayNames before calling it.
type geminiAPIRecorder struct{ requests []*adkmodel.LLMRequest }

func (*geminiAPIRecorder) Name() string                       { return "recorder" }
func (*geminiAPIRecorder) GetGoogleLLMVariant() genai.Backend { return genai.BackendGeminiAPI }
func (r *geminiAPIRecorder) GenerateContent(_ context.Context, req *adkmodel.LLMRequest, _ bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	r.requests = append(r.requests, req)
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		yield(&adkmodel.LLMResponse{Content: genai.NewContentFromText("ok", genai.RoleModel)}, nil)
	}
}

func fileA2APart(name, mediaType, data string) *a2atype.Part {
	return &a2atype.Part{Content: a2atype.Raw([]byte(data)), Filename: name, MediaType: mediaType}
}

// userTexts returns the text of each user content in the request.
func userTexts(req *adkmodel.LLMRequest) []string {
	var out []string
	for _, c := range req.Contents {
		if c.Role != genai.RoleUser {
			continue
		}
		var b strings.Builder
		for _, p := range c.Parts {
			b.WriteString(p.Text + "\n")
		}
		out = append(out, b.String())
	}
	return out
}

// Runs real A2A file parts through the converter, ADK's preprocessing and the file wrapper.
func TestUploadedFilesKeepTheirNamesThroughRunner(t *testing.T) {
	ctx := t.Context()
	rec := &geminiAPIRecorder{}
	a, err := llmagent.New(llmagent.Config{Name: "files", Model: fileextract.WithFileText(rec)})
	if err != nil {
		t.Fatal(err)
	}
	sessions := adksession.InMemoryService()
	r, err := runner.New(runner.Config{AppName: "test", Agent: a, SessionService: sessions})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(ctx, &adksession.CreateRequest{AppName: "test", UserID: "user"})
	if err != nil {
		t.Fatal(err)
	}
	send := func(parts ...*a2atype.Part) {
		t.Helper()
		msg := &genai.Content{Role: genai.RoleUser}
		for _, p := range parts {
			converted, err := a2aPartConverter(ctx, nil, p)
			if err != nil {
				t.Fatal(err)
			}
			msg.Parts = append(msg.Parts, converted)
		}
		for _, err := range r.Run(ctx, "user", sess.Session.ID(), msg, adkagent.RunConfig{}) {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	const csv = "id,v\n1,a"
	send(a2atype.NewTextPart("compare these"),
		fileA2APart("before.csv", "text/csv", csv),
		fileA2APart("after.csv", "text/csv", csv),
		fileA2APart("notes.txt", "application/octet-stream", "remember the milk"))
	send(a2atype.NewTextPart("and this one"), fileA2APart("renamed.csv", "text/csv", csv))

	if len(rec.requests) != 2 {
		t.Fatalf("model called %d times, want 2", len(rec.requests))
	}
	first := userTexts(rec.requests[0])
	later := userTexts(rec.requests[1])
	if len(first) != 1 || len(later) != 2 {
		t.Fatalf("user contents = %d then %d, want 1 then 2", len(first), len(later))
	}
	turn1 := []string{
		"Contents of uploaded file \"before.csv\":\n\n" + csv,
		"Contents of uploaded file \"after.csv\":\n\n" + csv,
		"Contents of uploaded file \"notes.txt\":\n\nremember the milk",
	}
	for _, text := range []string{first[0], later[0]} {
		for _, want := range turn1 {
			if !strings.Contains(text, want) {
				t.Errorf("first turn missing %q:\n%s", want, text)
			}
		}
		if strings.Contains(text, "renamed.csv") {
			t.Errorf("first turn took the later upload's name:\n%s", text)
		}
	}
	if want := "Contents of uploaded file \"renamed.csv\":\n\n" + csv; !strings.Contains(later[1], want) {
		t.Errorf("second turn missing %q:\n%s", want, later[1])
	}
}
