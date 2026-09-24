package fileextract

import (
	"context"
	"iter"
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
	history := &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "hi"}}}
	user := &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "explain"}, csv, image}}
	req := &model.LLMRequest{Contents: []*genai.Content{history, user}}

	inner := &recordingLLM{}
	for range WithFileText(inner).GenerateContent(t.Context(), req, false) {
	}

	got := inner.got.Contents
	if got[0] != history {
		t.Error("unchanged content was copied")
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
	req := &model.LLMRequest{Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}}}
	inner := &recordingLLM{}
	for range WithFileText(inner).GenerateContent(t.Context(), req, false) {
	}
	if inner.got != req {
		t.Error("request without files was copied")
	}
}
