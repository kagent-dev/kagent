package a2a

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"google.golang.org/genai"
)

// convertA2AMessageToGenAIContent converts an A2A Message to genai.Content.
func convertA2AMessageToGenAIContent(msg *a2atype.Message) (*genai.Content, error) {
	if msg == nil {
		return nil, fmt.Errorf("message is nil")
	}

	parts := make([]*genai.Part, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case a2atype.TextPart:
			parts = append(parts, genai.NewPartFromText(p.Text))
		case a2atype.FilePart:
			genaiPart := convertA2AFilePartToGenAI(p)
			if genaiPart != nil {
				parts = append(parts, genaiPart)
			}
		case *a2atype.DataPart:
			genaiPart := convertA2ADataPartToGenAI(p)
			if genaiPart != nil {
				parts = append(parts, genaiPart)
			}
		}
	}

	role := "user"
	if msg.Role == a2atype.MessageRoleAgent {
		role = "model"
	}

	return &genai.Content{
		Role:  role,
		Parts: parts,
	}, nil
}

func convertA2AFilePartToGenAI(p a2atype.FilePart) *genai.Part {
	if p.File == nil {
		return nil
	}
	if uriFile, ok := p.File.(a2atype.FileURI); ok {
		return genai.NewPartFromURI(uriFile.URI, uriFile.FileMeta.MimeType)
	}
	if bytesFile, ok := p.File.(a2atype.FileBytes); ok {
		data, err := base64.StdEncoding.DecodeString(bytesFile.Bytes)
		if err != nil {
			return nil
		}
		return genai.NewPartFromBytes(data, bytesFile.FileMeta.MimeType)
	}
	return nil
}

func convertA2ADataPartToGenAI(p *a2atype.DataPart) *genai.Part {
	if p == nil {
		return nil
	}
	if p.Metadata != nil {
		if partType, ok := p.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataTypeKey)].(string); ok {
			switch partType {
			case A2ADataPartMetadataTypeFunctionCall:
				name, _ := p.Data[PartKeyName].(string)
				funcArgs, _ := p.Data[PartKeyArgs].(map[string]any)
				if name != "" {
					genaiPart := genai.NewPartFromFunctionCall(name, funcArgs)
					if id, ok := p.Data[PartKeyID].(string); ok && id != "" {
						genaiPart.FunctionCall.ID = id
					}
					return genaiPart
				}
			case A2ADataPartMetadataTypeFunctionResponse:
				name, _ := p.Data[PartKeyName].(string)
				response, _ := p.Data[PartKeyResponse].(map[string]any)
				if name != "" {
					genaiPart := genai.NewPartFromFunctionResponse(name, response)
					if id, ok := p.Data[PartKeyID].(string); ok && id != "" {
						genaiPart.FunctionResponse.ID = id
					}
					return genaiPart
				}
			default:
				dataJSON, err := json.Marshal(p.Data)
				if err == nil {
					return genai.NewPartFromText(string(dataJSON))
				}
			}
			return nil
		}
	}
	dataJSON, err := json.Marshal(p.Data)
	if err == nil {
		return genai.NewPartFromText(string(dataJSON))
	}
	return nil
}

// formatRunnerError returns a user-facing error message and code for runner errors.
func formatRunnerError(err error) (errorMessage, errorCode string) {
	if err == nil {
		return "", ""
	}
	errorMessage = err.Error()
	errorCode = "RUNNER_ERROR"

	if containsAny(errorMessage, []string{
		"failed to extract tools",
		"failed to get MCP session",
		"failed to init MCP session",
		"connection failed",
		"context deadline exceeded",
		"Client.Timeout exceeded",
	}) {
		errorCode = "MCP_CONNECTION_ERROR"
		errorMessage = fmt.Sprintf(
			"MCP connection failure or timeout. This can happen if the MCP server is unreachable or slow to respond. "+
				"Please verify your MCP server is running and accessible. Original error: %s",
			err.Error(),
		)
	} else if containsAny(errorMessage, []string{
		"Name or service not known",
		"no such host",
		"DNS",
	}) {
		errorCode = "MCP_DNS_ERROR"
		errorMessage = fmt.Sprintf(
			"DNS resolution failure for MCP server: %s. "+
				"Please check if the MCP server address is correct and reachable within the cluster.",
			err.Error(),
		)
	} else if containsAny(errorMessage, []string{
		"Connection refused",
		"connect: connection refused",
		"ECONNREFUSED",
	}) {
		errorCode = "MCP_CONNECTION_REFUSED"
		errorMessage = fmt.Sprintf(
			"Failed to connect to MCP server: %s. "+
				"The server might be down or blocked by network policies.",
			err.Error(),
		)
	}
	return errorMessage, errorCode
}

// containsAny checks if the string contains any of the substrings (case-insensitive).
func containsAny(s string, substrings []string) bool {
	lowerS := strings.ToLower(s)
	for _, substr := range substrings {
		if strings.Contains(lowerS, strings.ToLower(substr)) {
			return true
		}
	}
	return false
}
