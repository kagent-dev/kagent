// Package models: xAI model implementing Google ADK model.LLM by delegating
// to the OpenAI adapter (xAI uses the OpenAI wire protocol).
package models

import (
	"context"
	"iter"

	"google.golang.org/adk/v2/model"
)

// Name implements model.LLM.
func (m *XAIModel) Name() string {
	return m.Config.Model
}

// GenerateContent implements model.LLM by delegating to the inner OpenAI
// adapter. xAI speaks the OpenAI chat/completions wire protocol, so message
// conversion, tool schemas, streaming, and telemetry are identical.
func (m *XAIModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return m.inner.GenerateContent(ctx, req, stream)
}
