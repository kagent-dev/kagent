package core

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// ConvertA2ARequestToRunArgs converts an A2A request to internal agent run arguments.
// This matches the Python implementation's convert_a2a_request_to_adk_run_args function
func ConvertA2ARequestToRunArgs(req *protocol.SendMessageParams, userID, sessionID string) map[string]interface{} {
	if req == nil {
		// Return minimal args if request is nil (matching Python: raises ValueError)
		return map[string]interface{}{
			ArgKeyUserID:    userID,
			ArgKeySessionID: sessionID,
		}
	}

	args := make(map[string]interface{})

	// Set user_id (matching Python: _get_user_id(request))
	args[ArgKeyUserID] = userID
	args[ArgKeySessionID] = sessionID

	// Convert A2A message parts to GenAI format (matching Python: convert_a2a_part_to_genai_part)
	var genaiParts []map[string]interface{}
	if req.Message.Parts == nil {
		// No parts to convert
		args[ArgKeyNewMessage] = map[string]interface{}{
			PartKeyRole:  "user",
			PartKeyParts: genaiParts,
		}
		args[ArgKeyMessage] = req.Message
		args[ArgKeyRunConfig] = map[string]interface{}{
			RunConfigKeyStreamingMode: "NONE",
		}
		return args
	}
	for _, part := range req.Message.Parts {
		genaiPart, err := ConvertA2APartToGenAIPart(part)
		if err != nil {
			// Log error but continue with other parts
			continue
		}
		if genaiPart != nil {
			genaiParts = append(genaiParts, genaiPart)
		}
	}

	// Create Content object (matching Python: genai_types.Content(role="user", parts=[...]))
	args[ArgKeyNewMessage] = map[string]interface{}{
		PartKeyRole:  "user",
		PartKeyParts: genaiParts,
	}
	// Also set as message for compatibility
	args[ArgKeyMessage] = req.Message

	// Extract streaming mode from request if available
	// In Python: RunConfig(streaming_mode=StreamingMode.SSE if stream else StreamingMode.NONE)
	// For now, we'll set a default - the executor config will determine actual streaming mode
	args[ArgKeyRunConfig] = map[string]interface{}{
		RunConfigKeyStreamingMode: "NONE", // Default, will be overridden by executor config
	}

	return args
}

// ConvertA2APartToGenAIPart converts an A2A Part to a GenAI Part (placeholder for now)
// In a full implementation, this would convert to Google GenAI types
func ConvertA2APartToGenAIPart(a2aPart protocol.Part) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	switch part := a2aPart.(type) {
	case *protocol.TextPart:
		result[PartKeyText] = part.Text
		return result, nil

	case *protocol.FilePart:
		if part.File != nil {
			if uriFile, ok := part.File.(*protocol.FileWithURI); ok {
				mimeType := ""
				if uriFile.MimeType != nil {
					mimeType = *uriFile.MimeType
				}
				result[PartKeyFileData] = map[string]interface{}{
					PartKeyFileURI:  uriFile.URI,
					PartKeyMimeType: mimeType,
				}
				return result, nil
			}
			if bytesFile, ok := part.File.(*protocol.FileWithBytes); ok {
				data, err := base64.StdEncoding.DecodeString(bytesFile.Bytes)
				if err != nil {
					return nil, fmt.Errorf("failed to decode base64 file data: %w", err)
				}
				mimeType := ""
				if bytesFile.MimeType != nil {
					mimeType = *bytesFile.MimeType
				}
				result[PartKeyInlineData] = map[string]interface{}{
					"data":          data,
					PartKeyMimeType: mimeType,
				}
				return result, nil
			}
		}
		return nil, fmt.Errorf("unsupported file part type")

	case *protocol.DataPart:
		// Check metadata for special types
		if part.Metadata != nil {
			if partType, ok := part.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataTypeKey)].(string); ok {
				switch partType {
				case A2ADataPartMetadataTypeFunctionCall:
					result[PartKeyFunctionCall] = part.Data
					return result, nil
				case A2ADataPartMetadataTypeFunctionResponse:
					result[PartKeyFunctionResponse] = part.Data
					return result, nil
				case A2ADataPartMetadataTypeCodeExecutionResult:
					result["code_execution_result"] = part.Data
					return result, nil
				case A2ADataPartMetadataTypeExecutableCode:
					result["executable_code"] = part.Data
					return result, nil
				}
			}
		}
		// Default: convert to JSON text
		dataJSON, err := json.Marshal(part.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal data part: %w", err)
		}
		result[PartKeyText] = string(dataJSON)
		return result, nil

	default:
		return nil, fmt.Errorf("unsupported part type: %T", a2aPart)
	}
}
