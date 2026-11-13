package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

const titlePrompt = "Write a title of 3 to 6 words for a chat that starts with the message below. " +
	"Use the language of the message. Reply with the title only: no quotes and no trailing punctuation.\n\n" +
	"Message:\n"

// Titler writes short chat titles with the agent's own model.
type Titler struct {
	llm    adkmodel.LLM
	config *genai.GenerateContentConfig
}

// NewTitler builds a Titler on the agent's configured model.
func NewTitler(ctx context.Context, modelConfig *configv1.ADKModelConfig) (*Titler, error) {
	if modelConfig == nil {
		return nil, errors.New("title generation needs a model")
	}
	llm, err := CreateLLM(ctx, modelConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create title model: %w", err)
	}
	return &Titler{llm: llm, config: generateContentConfig(modelConfig)}, nil
}

// Title returns the model's raw title for the user's first message.
func (t *Titler) Title(ctx context.Context, userMessage string) (string, error) {
	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText(titlePrompt+userMessage, genai.RoleUser)},
		Config:   t.config,
	}
	var title strings.Builder
	for resp, err := range t.llm.GenerateContent(ctx, req, false) {
		if err != nil {
			return "", fmt.Errorf("failed to generate title: %w", err)
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part != nil && !part.Thought {
					title.WriteString(part.Text)
				}
			}
		}
	}
	return title.String(), nil
}

const recapPrompt = "Write a recap of one or two short sentences for the person who is reading the chat below: " +
	"where things stand, and what comes next. " +
	`Always address the reader as "you" and refer to the assistant as "I"; never call them "the user". ` +
	`For example: "You asked about X and I answered Y. Next, you could do Z." ` +
	"Use the language of the conversation. " +
	"Reply with plain text only: no markdown, no headings, no lists.\n\n" +
	"Conversation:\n"

// Recapper writes short chat recaps with the agent's own model.
type Recapper struct {
	llm    adkmodel.LLM
	config *genai.GenerateContentConfig
}

// NewRecapper builds a Recapper on the agent's configured model.
func NewRecapper(ctx context.Context, modelConfig *configv1.ADKModelConfig) (*Recapper, error) {
	if modelConfig == nil {
		return nil, errors.New("recap generation needs a model")
	}
	llm, err := CreateLLM(ctx, modelConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create recap model: %w", err)
	}
	return &Recapper{llm: llm, config: generateContentConfig(modelConfig)}, nil
}

// Recap returns the model's raw recap of the transcript.
func (r *Recapper) Recap(ctx context.Context, transcript string) (string, error) {
	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText(recapPrompt+transcript, genai.RoleUser)},
		Config:   r.config,
	}
	var recap strings.Builder
	for resp, err := range r.llm.GenerateContent(ctx, req, false) {
		if err != nil {
			return "", fmt.Errorf("failed to generate recap: %w", err)
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part != nil && !part.Thought {
					recap.WriteString(part.Text)
				}
			}
		}
	}
	return recap.String(), nil
}
