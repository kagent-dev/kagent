package converter

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"google.golang.org/genai"
)

// A2AMessageToGenAIContent converts A2A Message to genai.Content.
func A2AMessageToGenAIContent(msg *a2atype.Message) (*genai.Content, error) {
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
	if msg.Role == a2atype.MessageRoleAgent {
		role = "model"
	}

	return &genai.Content{
		Role:  role,
		Parts: parts,
	}, nil
}

// A2APartToGenAIPart converts a single A2A Part to genai.Part.
func A2APartToGenAIPart(part a2atype.Part) (*genai.Part, error) {
	switch p := part.(type) {
	case a2atype.TextPart:
		return genai.NewPartFromText(p.Text), nil
	case a2atype.FilePart:
		return convertFilePartToGenAI(p)
	case *a2atype.DataPart:
		return convertDataPartToGenAI(p)
	default:
		return nil, fmt.Errorf("unsupported part type: %T", part)
	}
}

func convertFilePartToGenAI(p a2atype.FilePart) (*genai.Part, error) {
	if p.File == nil {
		return nil, nil
	}
	if uriFile, ok := p.File.(a2atype.FileURI); ok {
		mimeType := uriFile.FileMeta.MimeType
		return genai.NewPartFromURI(uriFile.URI, mimeType), nil
	}
	if bytesFile, ok := p.File.(a2atype.FileBytes); ok {
		data, err := base64.StdEncoding.DecodeString(bytesFile.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 file data: %w", err)
		}
		mimeType := bytesFile.FileMeta.MimeType
		return genai.NewPartFromBytes(data, mimeType), nil
	}
	return nil, nil
}

func convertDataPartToGenAI(p *a2atype.DataPart) (*genai.Part, error) {
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
