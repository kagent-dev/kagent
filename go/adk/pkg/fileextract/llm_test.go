package fileextract

import (
	"context"
	"iter"
	"strings"
	"testing"

	"google.golang.org/adk/v2/model"
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

func TestWithFileText_NameFromPartMetadata(t *testing.T) {
	file := &genai.Part{
		InlineData:   &genai.Blob{Data: []byte("remember the milk"), MIMEType: "application/octet-stream"},
		PartMetadata: map[string]any{FilenameMetadataKey: "notes.txt"},
	}
	req := &model.LLMRequest{Contents: []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{file}}}}
	inner := &recordingLLM{}
	for range WithFileText(inner).GenerateContent(t.Context(), req, false) {
	}
	if want := "Contents of uploaded file \"notes.txt\":\n\nremember the milk"; inner.got.Contents[0].Parts[0].Text != want {
		t.Errorf("file part = %+v, want text %q", inner.got.Contents[0].Parts[0], want)
	}
}

type pdfRecordingLLM struct{ recordingLLM }

func (pdfRecordingLLM) SendsPDFBytes() bool { return true }

func TestWithFileText_KeepsPDFForPDFLLM(t *testing.T) {
	pdf := &genai.Part{InlineData: &genai.Blob{Data: []byte("%PDF-1.4"), MIMEType: "application/pdf", DisplayName: "report.pdf"}}
	txt := &genai.Part{InlineData: &genai.Blob{Data: []byte("remember the milk"), MIMEType: "text/plain", DisplayName: "notes.txt"}}
	req := &model.LLMRequest{Contents: []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{pdf, txt}}}}

	inner := &pdfRecordingLLM{}
	for range WithFileText(inner).GenerateContent(t.Context(), req, false) {
	}
	parts := inner.got.Contents[0].Parts
	if parts[0] != pdf {
		t.Errorf("PDF part = %+v, want it unchanged", parts[0])
	}
	if !strings.Contains(parts[1].Text, "remember the milk") || parts[1].InlineData != nil {
		t.Errorf("text file part = %+v, want text", parts[1])
	}

	plain := &recordingLLM{}
	for range WithFileText(plain).GenerateContent(t.Context(), req, false) {
	}
	if got := plain.got.Contents[0].Parts[0]; got.InlineData != nil || got.Text == "" {
		t.Errorf("PDF part for an LLM without SendsPDFBytes = %+v, want text", got)
	}
}

func TestIsPDF(t *testing.T) {
	for _, tt := range []struct {
		mime, name, data string
		want             bool
	}{
		{"application/pdf", "", "%PDF-1.4", true},
		{"Application/PDF", "x.bin", "%PDF-1.7", true},
		{"application/octet-stream", "Report.PDF", "%PDF-1.4", true},
		{"", "report.pdf", "%PDF-1.4", true},
		{"text/plain", "report.pdf", "%PDF-1.4", false},
		{"application/octet-stream", "report.txt", "%PDF-1.4", false},
		{"application/pdf", "report.pdf", "not a pdf", false},
		{"application/pdf", "report.pdf", "", false},
	} {
		if got := IsPDF(&genai.Blob{MIMEType: tt.mime, Data: []byte(tt.data)}, tt.name); got != tt.want {
			t.Errorf("IsPDF(%q, %q, %q) = %v, want %v", tt.mime, tt.name, tt.data, got, tt.want)
		}
	}
}
