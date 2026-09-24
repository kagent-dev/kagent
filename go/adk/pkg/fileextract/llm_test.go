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
