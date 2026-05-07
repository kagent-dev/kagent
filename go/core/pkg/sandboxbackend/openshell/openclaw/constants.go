package openclaw

const (
	// bootstrapSecretProviderID is the secrets.providers key written into openclaw.json.
	bootstrapSecretProviderID = "kagent"

	// DefaultInferenceBaseURL is the Model provider baseUrl when ModelConfig does not set an explicit upstream.
	DefaultInferenceBaseURL = "https://inference.local/v1"
)
