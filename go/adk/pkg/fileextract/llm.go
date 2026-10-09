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
// extracted text; images, and PDFs for a pdfLLM, pass through for the adapter to handle.
func WithFileText(llm model.LLM) model.LLM {
	p, _ := llm.(pdfLLM)
	w := fileTextLLM{llm, p != nil && p.SendsPDFBytes()}
	if g, ok := llm.(googleLLM); ok {
		return googleFileTextLLM{w, g}
	}
	return w
}

// googleLLM is the optional method ADK probes to pick Gemini API or Vertex AI behavior.
type googleLLM interface{ GetGoogleLLMVariant() genai.Backend }

// pdfLLM is the optional method of an adapter that sends PDFs natively. An adapter
// that returns true receives PDF blobs unchanged, with the PartMetadata the A2A
// converter adds, so it must ignore PartMetadata (Vertex rejects it).
type pdfLLM interface{ SendsPDFBytes() bool }

type fileTextLLM struct {
	model.LLM
	keepPDF bool
}

type googleFileTextLLM struct {
	fileTextLLM
	googleLLM
}

func (m fileTextLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return m.LLM.GenerateContent(ctx, filesToText(req, m.keepPDF), stream)
}

// FilenameMetadataKey is the PartMetadata key the A2A converter stores a file's name under.
const FilenameMetadataKey = "kagent_filename"

// PartFileName returns the name of p's inline file, from PartMetadata when ADK blanked
// DisplayName (it does for Gemini API models).
func PartFileName(p *genai.Part) string {
	if p.InlineData == nil {
		return ""
	}
	if p.InlineData.DisplayName != "" {
		return p.InlineData.DisplayName
	}
	name, _ := p.PartMetadata[FilenameMetadataKey].(string)
	return name
}

// IsPDF reports a PDF by its media type, or by its name when the media type is
// empty or generic. The bytes must carry the PDF header either way.
func IsPDF(b *genai.Blob, name string) bool {
	if b == nil || !bytes.HasPrefix(b.Data, []byte("%PDF-")) {
		return false
	}
	m := normalizeMIME(b.MIMEType)
	return m == "application/pdf" || (m == "" || m == "application/octet-stream") && strings.HasSuffix(strings.ToLower(name), ".pdf")
}

// isDataPart reports an A2A data part ADK passed on as a text blob, not a user file.
func isDataPart(b *genai.Blob) bool {
	data := bytes.TrimSpace(b.Data)
	return b.MIMEType == "text/plain" &&
		bytes.HasPrefix(data, []byte("<a2a_datapart_json>")) && bytes.HasSuffix(data, []byte("</a2a_datapart_json>"))
}

// filesToText returns req with user file blobs replaced by text, copying only the
// contents and parts it changes so the caller's session history is untouched. When
// keepPDF is true, PDF blobs stay as they are.
func filesToText(req *model.LLMRequest, keepPDF bool) *model.LLMRequest {
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
			if p == nil || p.InlineData == nil || strings.HasPrefix(p.InlineData.MIMEType, "image/") || isDataPart(p.InlineData) ||
				keepPDF && IsPDF(p.InlineData, PartFileName(p)) {
				continue
			}
			if parts == nil {
				parts = slices.Clone(c.Parts)
			}
			parts[j] = genai.NewPartFromText(inlineFileToText(p.InlineData, PartFileName(p)))
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
