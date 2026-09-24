package fileextract

import (
	"context"
	"iter"
	"slices"
	"strings"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// WithFileText wraps an LLM so every provider receives non-image file blobs as
// extracted text; images pass through for the adapter to handle.
func WithFileText(llm model.LLM) model.LLM {
	return fileTextLLM{llm}
}

type fileTextLLM struct{ model.LLM }

func (m fileTextLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return m.LLM.GenerateContent(ctx, filesToText(req), stream)
}

// filesToText returns req with file blobs replaced by text, copying only the
// contents and parts it changes so the caller's session history is untouched.
func filesToText(req *model.LLMRequest) *model.LLMRequest {
	var contents []*genai.Content
	for i, c := range req.Contents {
		if c == nil {
			continue
		}
		var parts []*genai.Part
		for j, p := range c.Parts {
			if p == nil || p.InlineData == nil || strings.HasPrefix(p.InlineData.MIMEType, "image/") {
				continue
			}
			if parts == nil {
				parts = slices.Clone(c.Parts)
			}
			parts[j] = genai.NewPartFromText(InlineFileToText(p.InlineData))
		}
		if parts == nil {
			continue
		}
		if contents == nil {
			contents = slices.Clone(req.Contents)
		}
		changed := *c
		changed.Parts = parts
		contents[i] = &changed
	}
	if contents == nil {
		return req
	}
	out := *req
	out.Contents = contents
	return &out
}
