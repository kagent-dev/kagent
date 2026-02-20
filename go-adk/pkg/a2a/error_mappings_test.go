package a2a

import "testing"

func TestGetErrorMessage(t *testing.T) {
	tests := []struct {
		name      string
		errorCode string
		want      string
	}{
		{
			name:      "MAX_TOKENS",
			errorCode: "MAX_TOKENS",
			want:      "Response was truncated due to maximum token limit. Try asking a shorter question or breaking it into parts.",
		},
		{
			name:      "SAFETY",
			errorCode: "SAFETY",
			want:      "Response was blocked due to safety concerns. Please rephrase your request to avoid potentially harmful content.",
		},
		{
			name:      "RECITATION",
			errorCode: "RECITATION",
			want:      "Response was blocked due to unauthorized citations. Please rephrase your request.",
		},
		{
			name:      "BLOCKLIST",
			errorCode: "BLOCKLIST",
			want:      "Response was blocked due to restricted terminology. Please rephrase your request using different words.",
		},
		{
			name:      "PROHIBITED_CONTENT",
			errorCode: "PROHIBITED_CONTENT",
			want:      "Response was blocked due to prohibited content. Please rephrase your request.",
		},
		{
			name:      "SPII",
			errorCode: "SPII",
			want:      "Response was blocked due to sensitive personal information concerns. Please avoid including personal details.",
		},
		{
			name:      "MALFORMED_FUNCTION_CALL",
			errorCode: "MALFORMED_FUNCTION_CALL",
			want:      "The agent generated an invalid function call. This may be due to complex input data. Try rephrasing your request or breaking it into simpler steps.",
		},
		{
			name:      "OTHER",
			errorCode: "OTHER",
			want:      "An unexpected error occurred during processing. Please try again or rephrase your request.",
		},
		{
			name:      "unknown error code",
			errorCode: "UNKNOWN_ERROR",
			want:      "An error occurred during processing",
		},
		{
			name:      "empty error code",
			errorCode: "",
			want:      "An error occurred during processing",
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
