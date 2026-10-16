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

// WithFileText wraps an LLM so user files that the provider cannot read reach it as text.
// Images, and parts the adapter sends itself (see FileSender), are not changed.
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

// newestUserTurn is the index of the last user content, or -1. Only a file there
// may fail the request: one in replayed history would fail every later turn.
// ADK sends tool results as user content, so those are not a user turn.
func newestUserTurn(contents []*genai.Content) int {
	for i, c := range slices.Backward(contents) {
		if c != nil && strings.TrimSpace(c.Role) == genai.RoleUser &&
			!slices.ContainsFunc(c.Parts, func(p *genai.Part) bool { return p != nil && p.FunctionResponse != nil }) {
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

// filesToText returns req with the files in each user turn changed to a form the
// provider reads (see turnFilesToText). It never changes req, which holds the
// session history, and returns req itself when there is nothing to change.
func filesToText(req *model.LLMRequest, llm model.LLM) *model.LLMRequest {
	if req == nil {
		return req
	}
	out := *req
	out.Contents = slices.Clone(req.Contents)
	newest := newestUserTurn(req.Contents)
	for i, c := range req.Contents {
		if c == nil || c.Role != genai.RoleUser {
			continue
		}
		if parts := turnFilesToText(c.Parts, llm, i == newest); !slices.Equal(parts, c.Parts) {
			turn := *c
			turn.Parts = parts
			out.Contents[i] = &turn
		}
	}
	if slices.Equal(out.Contents, req.Contents) {
		return req
	}
	return &out
}

// turnFilesToText returns a new list of the parts of one user turn, with each file
// that the provider cannot read changed to text. A link stays a link only in the
// newest user turn: the provider fetches it on each request, so a link that breaks
// in an older turn would fail every later turn.
func turnFilesToText(parts []*genai.Part, llm model.LLM, newest bool) []*genai.Part {
	var out []*genai.Part
	for _, p := range parts {
		switch {
		case p == nil || providerAccepts(llm, p) && (newest || p.FileData == nil):
			// The model's adapter sends it as it is.
			out = append(out, p)
		case p.FileData != nil:
			// A link: the bare URL, so the agent's fetch tool can read it, or nothing when
			// the text already has the URL. Not "File: name (url)": with that, models
			// invented the file's contents.
			if !hasText(parts, p.FileData.FileURI) {
				out = append(out, genai.NewPartFromText(p.FileData.FileURI))
			}
		case p.InlineData != nil && !strings.HasPrefix(p.InlineData.MIMEType, "image/") && !isDataPart(p.InlineData):
			// A document as bytes: its text, or a note that it could not be read.
			out = append(out, genai.NewPartFromText(inlineFileToText(p.InlineData, partFileName(p))))
		default:
			// Text, images, A2A data parts and tool calls.
			out = append(out, p)
		}
	}
	return out
}

// hasText reports whether a text part in parts contains s.
func hasText(parts []*genai.Part, s string) bool {
	return slices.ContainsFunc(parts, func(p *genai.Part) bool { return p != nil && strings.Contains(p.Text, s) })
}
