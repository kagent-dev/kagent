package genai

import "testing"

func TestGetErrorMessage(t *testing.T) {
	tests := []struct {
		name      string
		errorCode string
		want      string
	}{
		{
			name:      "MAX_TOKENS",
			errorCode: FinishReasonMaxTokens,
			want:      "Response was truncated due to maximum token limit. Try asking a shorter question or breaking it into parts.",
		},
		{
			name:      "SAFETY",
			errorCode: FinishReasonSafety,
			want:      "Response was blocked due to safety concerns. Please rephrase your request to avoid potentially harmful content.",
		},
		{
			name:      "RECITATION",
			errorCode: FinishReasonRecitation,
			want:      "Response was blocked due to unauthorized citations. Please rephrase your request.",
		},
		{
			name:      "BLOCKLIST",
			errorCode: FinishReasonBlocklist,
			want:      "Response was blocked due to restricted terminology. Please rephrase your request using different words.",
		},
		{
			name:      "PROHIBITED_CONTENT",
			errorCode: FinishReasonProhibitedContent,
			want:      "Response was blocked due to prohibited content. Please rephrase your request.",
		},
		{
			name:      "SPII",
			errorCode: FinishReasonSPII,
			want:      "Response was blocked due to sensitive personal information concerns. Please avoid including personal details.",
		},
		{
			name:      "MALFORMED_FUNCTION_CALL",
			errorCode: FinishReasonMalformedFunctionCall,
			want:      "The agent generated an invalid function call. This may be due to complex input data. Try rephrasing your request or breaking it into simpler steps.",
		},
		{
			name:      "OTHER",
			errorCode: FinishReasonOther,
			want:      "An unexpected error occurred during processing. Please try again or rephrase your request.",
		},
		{
			name:      "unknown error code",
			errorCode: "UNKNOWN_ERROR",
			want:      defaultErrorMessage,
		},
		{
			name:      "empty error code",
			errorCode: "",
			want:      defaultErrorMessage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetErrorMessage(tt.errorCode)
			if got != tt.want {
				t.Errorf("GetErrorMessage(%q) = %q, want %q", tt.errorCode, got, tt.want)
			}
		})
	}
}

func TestIsNormalCompletion(t *testing.T) {
	tests := []struct {
		name      string
		errorCode string
		want      bool
	}{
		{
			name:      "STOP is normal completion",
			errorCode: FinishReasonStop,
			want:      true,
		},
		{
			name:      "MAX_TOKENS is not normal completion",
			errorCode: FinishReasonMaxTokens,
			want:      false,
		},
		{
			name:      "SAFETY is not normal completion",
			errorCode: FinishReasonSafety,
			want:      false,
		},
		{
			name:      "MALFORMED_FUNCTION_CALL is not normal completion",
			errorCode: FinishReasonMalformedFunctionCall,
			want:      false,
		},
		{
			name:      "unknown error code is not normal completion",
			errorCode: "UNKNOWN_ERROR",
			want:      false,
		},
		{
			name:      "empty error code is not normal completion",
			errorCode: "",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsNormalCompletion(tt.errorCode)
			if got != tt.want {
				t.Errorf("IsNormalCompletion(%q) = %v, want %v", tt.errorCode, got, tt.want)
			}
		})
	}
}
