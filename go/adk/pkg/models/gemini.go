// Package models: helpers for the native Gemini / Gemini Vertex AI models.
//
// The ADK Gemini model reads its generation config from the per-request
// LLMRequest.Config rather than from the model definition, so a
// ModelConfig-level setting such as maxOutputTokens is not otherwise applied.
// geminiGenerationConfigModel wraps the ADK model and injects the setting into
// each request, mirroring the Python _GeminiGenerationConfigMixin.
package models

import (
	"context"
	"iter"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// geminiGenerationConfigModel wraps an ADK Gemini model.LLM and applies a
// ModelConfig-level generation config (currently max_output_tokens) to each
// request. A per-request value always wins: the cap is only applied when the
// request does not already set MaxOutputTokens.
type geminiGenerationConfigModel struct {
	model.LLM
	maxOutputTokens int32
}

// WrapGeminiWithGenerationConfig wraps inner so that maxOutputTokens is applied
// to requests that don't already set it. When maxOutputTokens is nil or not
// positive, inner is returned unchanged.
func WrapGeminiWithGenerationConfig(inner model.LLM, maxOutputTokens *int) model.LLM {
	if maxOutputTokens == nil || *maxOutputTokens <= 0 {
		return inner
	}
	return &geminiGenerationConfigModel{
		LLM:             inner,
		maxOutputTokens: int32(*maxOutputTokens),
	}
}

// GenerateContent injects the configured max_output_tokens into the request
// config, without overriding a value the request already set, then delegates to
// the wrapped model.
func (m *geminiGenerationConfigModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	if req != nil && m.maxOutputTokens > 0 {
		if req.Config == nil {
			req.Config = &genai.GenerateContentConfig{}
		}
		if req.Config.MaxOutputTokens == 0 {
			req.Config.MaxOutputTokens = m.maxOutputTokens
		}
	}
	return m.LLM.GenerateContent(ctx, req, stream)
}
