package models

import (
	"context"
	"iter"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// fakeLLM records the request it was called with and returns an empty stream.
type fakeLLM struct {
	name    string
	gotReq  *model.LLMRequest
	gotCall bool
}

func (f *fakeLLM) Name() string { return f.name }

func (f *fakeLLM) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	f.gotCall = true
	f.gotReq = req
	return func(yield func(*model.LLMResponse, error) bool) {}
}

func drain(seq iter.Seq2[*model.LLMResponse, error]) {
	for range seq { //nolint:revive // consume the iterator
	}
}

func intPtr(i int) *int { return &i }

func TestWrapGeminiWithGenerationConfig_NoOpWhenUnset(t *testing.T) {
	inner := &fakeLLM{name: "gemini-2.0-flash"}
	for _, tc := range []struct {
		name string
		max  *int
	}{
		{"nil", nil},
		{"zero", intPtr(0)},
		{"negative", intPtr(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := WrapGeminiWithGenerationConfig(inner, tc.max)
			if got != model.LLM(inner) {
				t.Errorf("expected inner model returned unchanged, got wrapper %T", got)
			}
		})
	}
}

func TestGeminiGenerationConfig_AppliesCapWhenUnset(t *testing.T) {
	inner := &fakeLLM{name: "gemini-2.0-flash"}
	wrapped := WrapGeminiWithGenerationConfig(inner, intPtr(1024))

	req := &model.LLMRequest{Config: &genai.GenerateContentConfig{}}
	drain(wrapped.GenerateContent(context.Background(), req, false))

	if !inner.gotCall {
		t.Fatal("wrapped model did not delegate to inner")
	}
	if got := inner.gotReq.Config.MaxOutputTokens; got != 1024 {
		t.Errorf("MaxOutputTokens = %d, want 1024", got)
	}
}

func TestGeminiGenerationConfig_NilConfigInitialized(t *testing.T) {
	inner := &fakeLLM{name: "gemini-2.0-flash"}
	wrapped := WrapGeminiWithGenerationConfig(inner, intPtr(512))

	req := &model.LLMRequest{} // Config is nil
	drain(wrapped.GenerateContent(context.Background(), req, false))

	if inner.gotReq.Config == nil {
		t.Fatal("Config was not initialized")
	}
	if got := inner.gotReq.Config.MaxOutputTokens; got != 512 {
		t.Errorf("MaxOutputTokens = %d, want 512", got)
	}
}

func TestGeminiGenerationConfig_DoesNotOverridePerRequestValue(t *testing.T) {
	inner := &fakeLLM{name: "gemini-2.0-flash"}
	wrapped := WrapGeminiWithGenerationConfig(inner, intPtr(1024))

	req := &model.LLMRequest{Config: &genai.GenerateContentConfig{MaxOutputTokens: 256}}
	drain(wrapped.GenerateContent(context.Background(), req, false))

	if got := inner.gotReq.Config.MaxOutputTokens; got != 256 {
		t.Errorf("MaxOutputTokens = %d, want 256 (per-request value must win)", got)
	}
}

func TestGeminiGenerationConfig_NamePassthrough(t *testing.T) {
	inner := &fakeLLM{name: "gemini-2.0-flash"}
	wrapped := WrapGeminiWithGenerationConfig(inner, intPtr(1024))
	if got := wrapped.Name(); got != "gemini-2.0-flash" {
		t.Errorf("Name() = %q, want %q", got, "gemini-2.0-flash")
	}
}
