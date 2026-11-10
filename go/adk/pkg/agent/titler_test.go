package agent

import (
	"context"
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// thoughtModel replies with one thought part and one text part.
type thoughtModel struct{}

func (thoughtModel) Name() string { return "thought" }

func (thoughtModel) GenerateContent(context.Context, *adkmodel.LLMRequest, bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		yield(&adkmodel.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
			{Text: "let me think", Thought: true},
			{Text: "the answer"},
		}}}, nil)
	}
}

func TestTitler(t *testing.T) {
	t.Run("Given a model, when a title is requested, assert that the prompt carries the message and the reply is returned", func(t *testing.T) {
		model := &scriptedModel{}
		titler := &Titler{llm: model}

		title, err := titler.Title(t.Context(), "my login page is broken")

		require.NoError(t, err)
		assert.Equal(t, "reply 1", title)
		prompt := model.request(0).Contents[0].Parts[0].Text
		assert.Contains(t, prompt, titlePrompt)
		assert.Contains(t, prompt, "my login page is broken")
	})
}

func TestTitlerSkipsThoughts(t *testing.T) {
	t.Run("Given a model that returns a thought part, when a title is requested, assert that only the text part is returned", func(t *testing.T) {
		title, err := (&Titler{llm: thoughtModel{}}).Title(t.Context(), "hello")

		require.NoError(t, err)
		assert.Equal(t, "the answer", title)
	})
}
