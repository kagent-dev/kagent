package fileextract

import (
	"bytes"
	"context"
	"iter"
	"slices"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// WithFileText wraps an LLM so every provider receives non-image file blobs as
// extracted text; images pass through for the adapter to handle.
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
	return m.LLM.GenerateContent(ctx, filesToText(req, sessionUploads(ctx)), stream)
}

// sessionUploads returns the named user file blobs in the invocation's session.
// ADK blanks request DisplayNames for Gemini API models, but not the session's copies.
func sessionUploads(ctx context.Context) []*genai.Blob {
	ic, ok := ctx.(agent.InvocationContext)
	if !ok || ic.Session() == nil {
		return nil
	}
	var uploads []*genai.Blob
	for ev := range ic.Session().Events().All() {
		if ev == nil || ev.Content == nil || ev.Content.Role != genai.RoleUser {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p != nil && p.InlineData != nil && p.InlineData.DisplayName != "" {
				uploads = append(uploads, p.InlineData)
			}
		}
	}
	return uploads
}

// withUploadName returns b named after the latest identical upload when its name was blanked.
func withUploadName(b *genai.Blob, uploads []*genai.Blob) *genai.Blob {
	if b.DisplayName != "" {
		return b
	}
	for _, u := range slices.Backward(uploads) {
		if u.MIMEType == b.MIMEType && bytes.Equal(u.Data, b.Data) {
			named := *b
			named.DisplayName = u.DisplayName
			return &named
		}
	}
	return b
}

// isDataPart reports an A2A data part ADK passed on as a text blob, not a user file.
func isDataPart(b *genai.Blob) bool {
	data := bytes.TrimSpace(b.Data)
	return b.MIMEType == "text/plain" &&
		bytes.HasPrefix(data, []byte("<a2a_datapart_json>")) && bytes.HasSuffix(data, []byte("</a2a_datapart_json>"))
}

// filesToText returns req with user file blobs replaced by text, copying only the
// contents and parts it changes so the caller's session history is untouched.
func filesToText(req *model.LLMRequest, uploads []*genai.Blob) *model.LLMRequest {
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
			if p == nil || p.InlineData == nil || strings.HasPrefix(p.InlineData.MIMEType, "image/") || isDataPart(p.InlineData) {
				continue
			}
			if parts == nil {
				parts = slices.Clone(c.Parts)
			}
			parts[j] = genai.NewPartFromText(InlineFileToText(withUploadName(p.InlineData, uploads)))
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
