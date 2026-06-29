package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/genai"
)

// saveArtifactInput is the LLM-facing argument schema for the save_artifact tool.
type saveArtifactInput struct {
	Name     string `json:"name"`
	Content  string `json:"content"`
	MimeType string `json:"mime_type,omitempty"`
	Base64   bool   `json:"base64,omitempty"`
}

// NewSaveArtifactTool creates the save_artifact tool, letting an agent persist
// content as a downloadable file artifact in the current session. The artifact
// is auto-surfaced to the client as an A2A file part by the executor.
func NewSaveArtifactTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "save_artifact",
		Description: "Saves content as a downloadable file artifact in the current session so the " +
			"user receives it as a file attachment. Provide a file name (e.g. \"report.csv\"), the " +
			"file content, and optionally a MIME type (defaults to text/plain). For binary content, " +
			"base64-encode it and set base64=true.",
	}, func(toolCtx agent.ToolContext, in saveArtifactInput) (map[string]any, error) {
		return saveArtifact(toolCtx, toolCtx.Artifacts(), in, a2a.MaxArtifactBytes())
	})
}

// saveArtifact holds the testable core of the save_artifact tool: it validates
// the input, decodes the content, enforces the size limit, and stores the
// artifact as inline data so it round-trips to the UI as a file part.
func saveArtifact(ctx context.Context, artifacts agent.Artifacts, in saveArtifactInput, limit int) (map[string]any, error) {
	if artifacts == nil {
		return nil, fmt.Errorf("artifact service is not available")
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("missing required parameter: name")
	}
	if strings.ContainsAny(name, `/\`) {
		return nil, fmt.Errorf("invalid name %q: must not contain path separators", name)
	}

	var data []byte
	if in.Base64 {
		decoded, err := decodeFlexibleBase64(in.Content)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 content for artifact %q: %w", name, err)
		}
		data = decoded
	} else {
		data = []byte(in.Content)
	}

	if len(data) > limit {
		return nil, fmt.Errorf("artifact %q exceeds maximum allowed size: %d bytes > %d bytes", name, len(data), limit)
	}

	mimeType := strings.TrimSpace(in.MimeType)
	if mimeType == "" {
		mimeType = "text/plain"
	}

	part := &genai.Part{InlineData: &genai.Blob{
		Data:        data,
		MIMEType:    mimeType,
		DisplayName: name,
	}}

	resp, err := artifacts.Save(ctx, name, part)
	if err != nil {
		return nil, fmt.Errorf("failed to save artifact %q: %w", name, err)
	}

	return map[string]any{
		"status":     "saved",
		"name":       name,
		"version":    resp.Version,
		"mime_type":  mimeType,
		"size_bytes": len(data),
	}, nil
}

// decodeFlexibleBase64 decodes base64 using the standard or URL-safe alphabet,
// with or without padding. LLMs sometimes emit url-safe (-_) or unpadded base64;
// trying the common encodings avoids failing the tool call with a confusing
// "illegal base64 data" error for content that is in fact decodable.
func decodeFlexibleBase64(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("content is not valid base64 (tried standard and url-safe, padded and unpadded)")
}
