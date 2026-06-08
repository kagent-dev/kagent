package openclaw

import (
	"encoding/json"
)

// envSecretRef is OpenClaw's structured env SecretRef (see https://docs.openclaw.ai/gateway/secrets).
type envSecretRef struct {
	Source   string `json:"source"`
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

func openclawEnvSecretRef(envVar string) envSecretRef {
	return envSecretRef{
		Source:   "env",
		Provider: substrateSecretProviderID,
		ID:       envVar,
	}
}

// credentialValue marshals as either a plaintext string (OpenShell) or an OpenClaw env SecretRef (Substrate).
type credentialValue struct {
	literal   string
	envSecret *envSecretRef
}

func (c credentialValue) MarshalJSON() ([]byte, error) {
	if c.envSecret != nil {
		return json.Marshal(c.envSecret)
	}
	return json.Marshal(c.literal)
}
