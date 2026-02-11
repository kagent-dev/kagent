package a2a

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/genai"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// convertProtocolMessageToGenAIContent converts protocol.Message to genai.Content.
func convertProtocolMessageToGenAIContent(msg *protocol.Message) (*genai.Content, error) {
	if msg == nil {
		return nil, fmt.Errorf("message is nil")
	}

	parts := make([]*genai.Part, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case *protocol.TextPart:
			parts = append(parts, genai.NewPartFromText(p.Text))
		case protocol.TextPart:
			parts = append(parts, genai.NewPartFromText(p.Text))
		case *protocol.FilePart:
			if p.File != nil {
				if uriFile, ok := p.File.(*protocol.FileWithURI); ok {
					mimeType := ""
					if uriFile.MimeType != nil {
						mimeType = *uriFile.MimeType
					}
					parts = append(parts, genai.NewPartFromURI(uriFile.URI, mimeType))
				} else if bytesFile, ok := p.File.(*protocol.FileWithBytes); ok {
					data, err := base64.StdEncoding.DecodeString(bytesFile.Bytes)
					if err != nil {
						return nil, fmt.Errorf("failed to decode base64 file data: %w", err)
					}
					mimeType := ""
					if bytesFile.MimeType != nil {
						mimeType = *bytesFile.MimeType
					}
					parts = append(parts, genai.NewPartFromBytes(data, mimeType))
				}
			}
		case *protocol.DataPart:
			if p.Metadata != nil {
				if partType, ok := p.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataTypeKey)].(string); ok {
					switch partType {
					case A2ADataPartMetadataTypeFunctionCall:
						if funcCallData, ok := p.Data.(map[string]any); ok {
							name, _ := funcCallData["name"].(string)
							funcArgs, _ := funcCallData["args"].(map[string]any)
							if name != "" {
								genaiPart := genai.NewPartFromFunctionCall(name, funcArgs)
								if id, ok := funcCallData["id"].(string); ok && id != "" {
									genaiPart.FunctionCall.ID = id
								}
								parts = append(parts, genaiPart)
							}
						}
					case A2ADataPartMetadataTypeFunctionResponse:
						if funcRespData, ok := p.Data.(map[string]any); ok {
							name, _ := funcRespData["name"].(string)
							response, _ := funcRespData["response"].(map[string]any)
							if name != "" {
								genaiPart := genai.NewPartFromFunctionResponse(name, response)
								if id, ok := funcRespData["id"].(string); ok && id != "" {
									genaiPart.FunctionResponse.ID = id
								}
								parts = append(parts, genaiPart)
							}
						}
					default:
						dataJSON, err := json.Marshal(p.Data)
						if err == nil {
							parts = append(parts, genai.NewPartFromText(string(dataJSON)))
						}
					}
					continue
				}
			}
			dataJSON, err := json.Marshal(p.Data)
			if err == nil {
				parts = append(parts, genai.NewPartFromText(string(dataJSON)))
			}
		}
	}

	role := "user"
	if msg.Role == protocol.MessageRoleAgent {
		role = "model"
	}

	return &genai.Content{
		Role:  role,
		Parts: parts,
	}, nil
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
