package fileextract

import (
	"bytes"
	"context"
	"iter"
	"slices"
	"strings"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// WithFileText wraps an LLM so every provider receives non-image file blobs as
// extracted text, and links it cannot send as their URL in text.
// Images, and files the model's adapter sends itself (see FileSender), pass through.
func WithFileText(llm model.LLM) model.LLM {
	if g, ok := llm.(googleLLM); ok {
		return googleFileTextLLM{fileTextLLM{llm}, g}
	}
	return fileTextLLM{llm}
}

// googleLLM is the optional method ADK probes to pick Gemini API or Vertex AI behavior.
type googleLLM interface{ GetGoogleLLMVariant() genai.Backend }

type fileTextLLM struct{ model.LLM }

type googleFileTextLLM struct {
	fileTextLLM
	googleLLM
}

func (m fileTextLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return m.LLM.GenerateContent(ctx, filesToText(req, m.LLM), stream)
}

// FileSender is a model whose adapter sends some user file parts to its provider
// as they are: a document as bytes, or a link the provider fetches itself.
type FileSender interface{ ProviderAccepts(p *genai.Part) bool }

// providerAccepts reports whether llm's provider takes p as it is, through llm's adapter.
func providerAccepts(llm model.LLM, p *genai.Part) bool {
	if s, ok := llm.(FileSender); ok {
		return s.ProviderAccepts(p)
	}
	// Gemini takes a link as genai FileData, as it is.
	_, google := llm.(googleLLM)
	return google && p.FileData != nil
}

// NewestUserTurn is the index of the last user content, or -1. Only a file there
// may fail the request: one in replayed history would fail every later turn.
func NewestUserTurn(contents []*genai.Content) int {
	for i, c := range slices.Backward(contents) {
		if c != nil && strings.TrimSpace(c.Role) == genai.RoleUser {
			return i
		}
	}
	return -1
}

// FilenameMetadataKey is the PartMetadata key the A2A converter stores a file's name under.
const FilenameMetadataKey = "kagent_filename"

// partFileName returns the file's name, from PartMetadata when ADK blanked DisplayName
// (it does for Gemini API models).
func partFileName(p *genai.Part) string {
	if p.InlineData.DisplayName != "" {
		return p.InlineData.DisplayName
	}
	name, _ := p.PartMetadata[FilenameMetadataKey].(string)
	return name
}

// isDataPart reports an A2A data part ADK passed on as a text blob, not a user file.
func isDataPart(b *genai.Blob) bool {
	data := bytes.TrimSpace(b.Data)
	return b.MIMEType == "text/plain" &&
		bytes.HasPrefix(data, []byte("<a2a_datapart_json>")) && bytes.HasSuffix(data, []byte("</a2a_datapart_json>"))
}

// filesToText returns req with user files replaced by text, copying only the
// contents and parts it changes so the caller's session history is untouched.
func filesToText(req *model.LLMRequest, llm model.LLM) *model.LLMRequest {
	if req == nil {
		return req
	}
	var contents []*genai.Content
	for i, c := range req.Contents {
		if c == nil || c.Role != genai.RoleUser {
			continue
		}
		var parts []*genai.Part
		for j, p := range c.Parts {
			if p == nil || providerAccepts(llm, p) {
				continue
			}
			var text *genai.Part // nil drops the part
			switch {
			case p.FileData != nil:
				// The adapter would drop this link without a word. The model gets the bare URL,
				// as the user would type it, so the agent's fetch tool can read it. Not a
				// "File: name (url)" form: with that, models invented the file's contents.
				if !hasText(c.Parts, p.FileData.FileURI) {
					text = genai.NewPartFromText(p.FileData.FileURI)
				}
			case p.InlineData != nil && !strings.HasPrefix(p.InlineData.MIMEType, "image/") && !isDataPart(p.InlineData):
				text = genai.NewPartFromText(inlineFileToText(p.InlineData, partFileName(p)))
			default:
				continue
			}
			if parts == nil {
				parts = slices.Clone(c.Parts)
			}
			parts[j] = text
		}
		if parts == nil {
			continue
		}
		if contents == nil {
			contents = slices.Clone(req.Contents)
		}
		changed := *c
		changed.Parts = slices.DeleteFunc(parts, func(p *genai.Part) bool { return p == nil })
		contents[i] = &changed
	}
	if contents == nil {
		return req
	}
	out := *req
	out.Contents = contents
	return &out
}

// hasText reports whether a text part in parts contains s.
func hasText(parts []*genai.Part, s string) bool {
	return slices.ContainsFunc(parts, func(p *genai.Part) bool { return p != nil && strings.Contains(p.Text, s) })
}
