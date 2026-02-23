package a2a

const (
	StateKeySessionName = "session_name"

	requestEucFunctionCallName = "request_euc"
)

// A2A DataPart metadata keys and type values.
const (
	A2ADataPartMetadataTypeKey              = "type"
	A2ADataPartMetadataIsLongRunningKey     = "is_long_running"
	A2ADataPartMetadataTypeFunctionCall     = "function_call"
	A2ADataPartMetadataTypeFunctionResponse = "function_response"
)

// DataPart map keys for GenAI-style function call / response content.
const (
	PartKeyName     = "name"
	PartKeyArgs     = "args"
	PartKeyResponse = "response"
	PartKeyID       = "id"
)
