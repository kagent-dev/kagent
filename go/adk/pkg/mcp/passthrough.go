package mcp

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"strings"
	"unicode/utf8"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolutils"
	"google.golang.org/genai"

	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// passthroughToolset keeps only configuration, never a client session or caller
// credential. ADK's ordinary toolset shares one MCP session for its lifetime;
// that session can retain the first caller's identity for background requests.
type passthroughToolset struct {
	params mcpServerParams
	filter map[string]bool
}

func (*passthroughToolset) Name() string { return "mcp_tool_set" }

func (p *passthroughToolset) connect(ctx context.Context) (*mcpsdk.ClientSession, error) {
	transport, err := createTransport(ctx, p.params)
	if err != nil {
		return nil, err
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "kagent", Version: "1"},
		&mcpsdk.ClientOptions{Capabilities: mcpUIClientCapabilities()})
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		// Remote errors can echo request credentials.
		return nil, fmt.Errorf("failed to connect passthrough MCP server")
	}
	return session, nil
}

func closePassthroughSession(ctx context.Context, session *mcpsdk.ClientSession) {
	if err := session.Close(); err != nil {
		logging.FromContext(ctx).WarnContext(ctx, "failed to close passthrough MCP session")
	}
}

func (p *passthroughToolset) Tools(ctx adkagent.ReadonlyContext) ([]tool.Tool, error) {
	session, err := p.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer closePassthroughSession(ctx, session)
	var tools []tool.Tool
	for item, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("failed to list passthrough MCP tools")
		}
		if len(p.filter) > 0 && !p.filter[item.Name] || mcpToolKindOf(item.Meta) == mcpToolKindAppInternal {
			continue
		}
		declaration := &genai.FunctionDeclaration{Name: item.Name, Description: item.Description}
		if item.InputSchema != nil {
			declaration.ParametersJsonSchema = item.InputSchema
		}
		if item.OutputSchema != nil {
			declaration.ResponseJsonSchema = item.OutputSchema
		}
		tools = append(tools, &passthroughTool{set: p, declaration: declaration})
	}
	return tools, nil
}

type passthroughTool struct {
	set         *passthroughToolset
	declaration *genai.FunctionDeclaration
}

func (p *passthroughTool) Name() string                            { return p.declaration.Name }
func (p *passthroughTool) Description() string                     { return p.declaration.Description }
func (*passthroughTool) IsLongRunning() bool                       { return false }
func (p *passthroughTool) Declaration() *genai.FunctionDeclaration { return p.declaration }
func (p *passthroughTool) ProcessRequest(_ adkagent.Context, req *model.LLMRequest) error {
	return toolutils.PackTool(req, p)
}

func (p *passthroughTool) Run(ctx adkagent.Context, args any) (map[string]any, error) {
	session, err := p.set.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer closePassthroughSession(ctx, session)
	result, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: p.Name(), Arguments: args})
	if err != nil {
		return nil, fmt.Errorf("failed to call passthrough MCP tool")
	}
	if result.IsError {
		return nil, fmt.Errorf("passthrough MCP tool execution failed")
	}
	if result.StructuredContent != nil {
		return map[string]any{"output": result.StructuredContent}, nil
	}
	return map[string]any{"output": formatPassthroughContent(result.Content)}, nil
}

// The success formatter below follows google.golang.org/adk/v2 v2.5.0
// tool/mcptoolset/tool.go. Its converter is private, and using the ordinary
// toolset would retain the first caller's session. Keep error handling above
// separate: upstream error formatting can expose caller credentials.
//
// Adapted from Google ADK, Copyright 2025 Google LLC, under the Apache License,
// Version 2.0 (https://www.apache.org/licenses/LICENSE-2.0).
func formatPassthroughContent(contents []mcpsdk.Content) string {
	var result strings.Builder
	var previous string
	previousPlain := true
	for _, content := range contents {
		text, plain := formatPassthroughBlock(content)
		if text == "" {
			continue
		}
		if previous != "" && (!previousPlain || !plain) &&
			!strings.HasSuffix(previous, "\n") && !strings.HasPrefix(text, "\n") {
			result.WriteByte('\n')
		}
		result.WriteString(text)
		previous, previousPlain = text, plain
	}
	return result.String()
}

func formatPassthroughBlock(content mcpsdk.Content) (string, bool) {
	switch content := content.(type) {
	case nil:
		return "[MCP content: unavailable]", false
	case *mcpsdk.TextContent:
		if content == nil {
			return "[MCP text content: unavailable]", false
		}
		return content.Text, true
	case *mcpsdk.EmbeddedResource:
		if content == nil || content.Resource == nil {
			return "[MCP embedded resource: unavailable]", false
		}
		resource := content.Resource
		attributes := passthroughResourceAttributes(resource.URI, resource.MIMEType)
		body := resource.Text
		if body == "" {
			body, _ = decodePassthroughTextBlob(resource.Blob, resource.MIMEType)
		}
		if len(resource.Blob) > 0 && (resource.Text != "" || body == "") {
			attributes = append(attributes, fmt.Sprintf("size=%d bytes", len(resource.Blob)))
		}
		text := passthroughContentLabel("embedded resource", attributes)
		if body != "" {
			text += "\n" + body
		}
		return text, false
	case *mcpsdk.ResourceLink:
		if content == nil {
			return "[MCP resource link: unavailable]", false
		}
		attributes := passthroughResourceAttributes(content.URI, content.MIMEType)
		if content.Name != "" {
			attributes = append(attributes, fmt.Sprintf("name=%q", content.Name))
		}
		if content.Title != "" {
			attributes = append(attributes, fmt.Sprintf("title=%q", content.Title))
		}
		if content.Description != "" {
			attributes = append(attributes, fmt.Sprintf("description=%q", content.Description))
		}
		if content.Size != nil {
			attributes = append(attributes, fmt.Sprintf("size=%d bytes", *content.Size))
		}
		return passthroughContentLabel("resource link", attributes), false
	case *mcpsdk.ImageContent:
		if content == nil {
			return "[MCP image: unavailable]", false
		}
		return passthroughMediaLabel("image", content.MIMEType, len(content.Data)), false
	case *mcpsdk.AudioContent:
		if content == nil {
			return "[MCP audio: unavailable]", false
		}
		return passthroughMediaLabel("audio", content.MIMEType, len(content.Data)), false
	default:
		return "[MCP content: unsupported]", false
	}
}

func passthroughResourceAttributes(uri, mimeType string) []string {
	attributes := make([]string, 0, 2)
	if uri != "" {
		attributes = append(attributes, fmt.Sprintf("uri=%q", uri))
	}
	if mimeType != "" {
		attributes = append(attributes, fmt.Sprintf("mimeType=%q", mimeType))
	}
	return attributes
}

func passthroughMediaLabel(kind, mimeType string, size int) string {
	attributes := passthroughResourceAttributes("", mimeType)
	attributes = append(attributes, fmt.Sprintf("size=%d bytes", size))
	return passthroughContentLabel(kind, attributes)
}

func passthroughContentLabel(kind string, attributes []string) string {
	if len(attributes) == 0 {
		return "[MCP " + kind + "]"
	}
	return "[MCP " + kind + ": " + strings.Join(attributes, ", ") + "]"
}

func decodePassthroughTextBlob(blob []byte, mimeType string) (string, bool) {
	if len(blob) == 0 {
		return "", false
	}
	declaresCharset := passthroughHasMIMECharset(mimeType)
	mediaType, params, err := mime.ParseMediaType(mimeType)
	if err != nil && (!errors.Is(err, mime.ErrInvalidMediaParameter) || declaresCharset) {
		return "", false
	}
	if !passthroughTextMediaType(mediaType) {
		return "", false
	}
	charset, parsedCharset := params["charset"]
	if declaresCharset && !parsedCharset {
		return "", false
	}
	switch strings.ToLower(charset) {
	case "", "utf-8", "utf8":
		if !utf8.Valid(blob) {
			return "", false
		}
	case "us-ascii", "ascii":
		for _, b := range blob {
			if b >= utf8.RuneSelf {
				return "", false
			}
		}
	default:
		return "", false
	}
	return string(blob), true
}

func passthroughHasMIMECharset(value string) bool {
	_, params, ok := strings.Cut(value, ";")
	if !ok {
		return false
	}
	start := 0
	inQuote := false
	for i := 0; i <= len(params); i++ {
		if i != len(params) {
			switch params[i] {
			case '\\':
				if inQuote && i+1 < len(params) {
					i++
				}
				continue
			case '"':
				if inQuote {
					// Validate the quoted parameter before trusting its boundaries.
					if _, _, err := mime.ParseMediaType("text/plain;" + params[start:i+1]); err != nil {
						return true
					}
				} else {
					_, prefix, ok := strings.Cut(params[start:i], "=")
					if !ok || strings.TrimSpace(prefix) != "" {
						return true
					}
				}
				inQuote = !inQuote
				continue
			}
		}
		if i != len(params) && (params[i] != ';' || inQuote) {
			continue
		}
		name, _, _ := strings.Cut(params[start:i], "=")
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "charset" || strings.HasPrefix(name, "charset*") {
			return true
		}
		start = i + 1
	}
	return inQuote
}

func passthroughTextMediaType(mediaType string) bool {
	if strings.HasPrefix(mediaType, "text/") || strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml") {
		return true
	}
	switch mediaType {
	case "application/json", "application/javascript", "application/toml", "application/x-www-form-urlencoded", "application/xml",
		"application/x-yaml", "application/yaml":
		return true
	default:
		return false
	}
}

var _ tool.Toolset = (*passthroughToolset)(nil)
var _ functionTool = (*passthroughTool)(nil)
