package converter

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"google.golang.org/genai"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// A2AMessageToGenAIContent converts protocol.Message to genai.Content.
func A2AMessageToGenAIContent(msg *protocol.Message) (*genai.Content, error) {
	if msg == nil {
		return nil, fmt.Errorf("message is nil")
	}

	parts := make([]*genai.Part, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		genaiPart, err := A2APartToGenAIPart(part)
		if err != nil {
			continue // Skip parts that can't be converted
		}
		if genaiPart != nil {
			parts = append(parts, genaiPart)
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

// A2APartToGenAIPart converts a single A2A protocol.Part to genai.Part.
func A2APartToGenAIPart(part protocol.Part) (*genai.Part, error) {
	switch p := part.(type) {
	case *protocol.TextPart:
		return genai.NewPartFromText(p.Text), nil
	case protocol.TextPart:
		return genai.NewPartFromText(p.Text), nil
	case *protocol.FilePart:
		return convertFilePartToGenAI(p)
	case protocol.FilePart:
		return convertFilePartToGenAI(&p)
	case *protocol.DataPart:
		return convertDataPartToGenAI(p)
	case protocol.DataPart:
		return convertDataPartToGenAI(&p)
	default:
		return nil, fmt.Errorf("unsupported part type: %T", part)
	}
}

func convertFilePartToGenAI(p *protocol.FilePart) (*genai.Part, error) {
	if p.File == nil {
		return nil, nil
	}
	if uriFile, ok := p.File.(*protocol.FileWithURI); ok {
		mimeType := ""
		if uriFile.MimeType != nil {
			mimeType = *uriFile.MimeType
		}
		return genai.NewPartFromURI(uriFile.URI, mimeType), nil
	}
	if bytesFile, ok := p.File.(*protocol.FileWithBytes); ok {
		data, err := base64.StdEncoding.DecodeString(bytesFile.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 file data: %w", err)
		}
		mimeType := ""
		if bytesFile.MimeType != nil {
			mimeType = *bytesFile.MimeType
		}
		return genai.NewPartFromBytes(data, mimeType), nil
	}
	return nil, nil
}

func convertDataPartToGenAI(p *protocol.DataPart) (*genai.Part, error) {
	if p.Metadata != nil {
		if partType, ok := p.Metadata[a2a.MetadataKeyType].(string); ok {
			switch partType {
			case a2a.A2ADataPartMetadataTypeFunctionCall:
				return convertFunctionCallDataToGenAI(p.Data)
			case a2a.A2ADataPartMetadataTypeFunctionResponse:
				return convertFunctionResponseDataToGenAI(p.Data)
			}
		}
	}
	// Default: convert DataPart to JSON text
	dataJSON, err := json.Marshal(p.Data)
	if err != nil {
		return nil, err
	}
	return genai.NewPartFromText(string(dataJSON)), nil
}

func convertFunctionCallDataToGenAI(data interface{}) (*genai.Part, error) {
	funcCallData, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("function call data is not a map")
	}
	name, _ := funcCallData[a2a.PartKeyName].(string)
	args, _ := funcCallData[a2a.PartKeyArgs].(map[string]interface{})
	if name == "" {
		return nil, fmt.Errorf("function call missing name")
	}
	genaiPart := genai.NewPartFromFunctionCall(name, args)
	if id, ok := funcCallData[a2a.PartKeyID].(string); ok && id != "" {
		genaiPart.FunctionCall.ID = id
	}
	return genaiPart, nil
}

func convertFunctionResponseDataToGenAI(data interface{}) (*genai.Part, error) {
	funcRespData, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("function response data is not a map")
	}
	name, _ := funcRespData[a2a.PartKeyName].(string)
	response, _ := funcRespData[a2a.PartKeyResponse].(map[string]interface{})
	if name == "" {
		return nil, fmt.Errorf("function response missing name")
	}
	genaiPart := genai.NewPartFromFunctionResponse(name, response)
	if id, ok := funcRespData[a2a.PartKeyID].(string); ok && id != "" {
		genaiPart.FunctionResponse.ID = id
	}
	return genaiPart, nil
}
