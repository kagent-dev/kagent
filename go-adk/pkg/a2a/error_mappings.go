package a2a

// defaultErrorMessage is the fallback message for unrecognized error codes.
var defaultErrorMessage = "An error occurred during processing"

// Error code to user-friendly message mappings
var errorCodeMessages = map[string]string{
	"MAX_TOKENS":              "Response was truncated due to maximum token limit. Try asking a shorter question or breaking it into parts.",
	"SAFETY":                  "Response was blocked due to safety concerns. Please rephrase your request to avoid potentially harmful content.",
	"RECITATION":              "Response was blocked due to unauthorized citations. Please rephrase your request.",
	"BLOCKLIST":               "Response was blocked due to restricted terminology. Please rephrase your request using different words.",
	"PROHIBITED_CONTENT":      "Response was blocked due to prohibited content. Please rephrase your request.",
	"SPII":                    "Response was blocked due to sensitive personal information concerns. Please avoid including personal details.",
	"MALFORMED_FUNCTION_CALL": "The agent generated an invalid function call. This may be due to complex input data. Try rephrasing your request or breaking it into simpler steps.",
	"OTHER":                   "An unexpected error occurred during processing. Please try again or rephrase your request.",
}

// GetErrorMessage returns a user-friendly error message for the given error code.
func GetErrorMessage(errorCode string) string {
	if msg, ok := errorCodeMessages[errorCode]; ok {
		return msg
	}
	return defaultErrorMessage
}
