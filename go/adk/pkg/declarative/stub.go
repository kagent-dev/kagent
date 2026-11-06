package declarative

import (
	"context"
	"iter"
	"slices"
	"strings"

	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// stubModel answers every request with a deterministic echo of the last
// user text. It lets documents run in tests and examples without credentials.
type stubModel struct{ name string }

// NewStubModel returns an offline model that replies "[<name>] <last user
// text>". It never calls tools.
func NewStubModel(name string) adkmodel.LLM { return stubModel{name: name} }

func (m stubModel) Name() string { return m.name }

func (m stubModel) GenerateContent(_ context.Context, req *adkmodel.LLMRequest, _ bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		reply := "[" + m.name + "] " + lastUserText(req)
		yield(&adkmodel.LLMResponse{
			Content:      genai.NewContentFromText(reply, genai.RoleModel),
			TurnComplete: true,
		}, nil)
	}
}

func lastUserText(req *adkmodel.LLMRequest) string {
	if req == nil {
		return ""
	}
	for _, c := range slices.Backward(req.Contents) {
		if c == nil || c.Role != genai.RoleUser {
			continue
		}
		var sb strings.Builder
		for _, p := range c.Parts {
			if p != nil {
				sb.WriteString(p.Text)
			}
		}
		if sb.Len() > 0 {
			return sb.String()
		}
	}
	return ""
}
