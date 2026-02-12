package genai

// Error code constants (matching Google GenAI FinishReason)
const (
	FinishReasonStop                  = "STOP"
	FinishReasonMaxTokens             = "MAX_TOKENS"
	FinishReasonSafety                = "SAFETY"
	FinishReasonRecitation            = "RECITATION"
	FinishReasonBlocklist             = "BLOCKLIST"
	FinishReasonProhibitedContent     = "PROHIBITED_CONTENT"
	FinishReasonSPII                  = "SPII"
	FinishReasonMalformedFunctionCall = "MALFORMED_FUNCTION_CALL"
	FinishReasonOther                 = "OTHER"
)

// Error code to user-friendly message mappings
var errorCodeMessages = map[string]string{
	FinishReasonMaxTokens:             "Response was truncated due to maximum token limit. Try asking a shorter question or breaking it into parts.",
	FinishReasonSafety:                "Response was blocked due to safety concerns. Please rephrase your request to avoid potentially harmful content.",
	FinishReasonRecitation:            "Response was blocked due to unauthorized citations. Please rephrase your request.",
	FinishReasonBlocklist:             "Response was blocked due to restricted terminology. Please rephrase your request using different words.",
	FinishReasonProhibitedContent:     "Response was blocked due to prohibited content. Please rephrase your request.",
	FinishReasonSPII:                  "Response was blocked due to sensitive personal information concerns. Please avoid including personal details.",
	FinishReasonMalformedFunctionCall: "The agent generated an invalid function call. This may be due to complex input data. Try rephrasing your request or breaking it into simpler steps.",
	FinishReasonOther:                 "An unexpected error occurred during processing. Please try again or rephrase your request.",
}

// Normal completion reasons that should not be treated as errors
var normalCompletionReasons = map[string]bool{
	FinishReasonStop: true,
}

const defaultErrorMessage = "An error occurred during processing"

// DefaultErrorMessage is exported for use in other packages
var DefaultErrorMessage = defaultErrorMessage

// GetErrorMessage returns a user-friendly error message for the given error code
func GetErrorMessage(errorCode string) string {
	if msg, ok := errorCodeMessages[errorCode]; ok {
		return msg
	}
	return defaultErrorMessage
}

// IsNormalCompletion checks if the error code represents normal completion rather than an error
func IsNormalCompletion(errorCode string) bool {
	return normalCompletionReasons[errorCode]
}
