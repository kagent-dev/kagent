package config

import configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"

const (
	FoundryAPIFormatOpenAI    = "openai"
	FoundryAPIFormatAnthropic = "anthropic"
)

// ModelType is the selected provider's schema name.
func ModelType(m *configv1.ADKModelConfig) string {
	if m == nil {
		return ""
	}
	message := m.ProtoReflect()
	field := message.WhichOneof(message.Descriptor().Oneofs().ByName("provider"))
	if field == nil {
		return ""
	}
	return string(field.Name())
}

// ModelToEmbeddingConfig selects the model settings supported by memory providers.
func ModelToEmbeddingConfig(m *configv1.ADKModelConfig) *configv1.ADKEmbeddingConfig {
	if m == nil {
		return nil
	}
	e := &configv1.ADKEmbeddingConfig{Provider: ModelType(m), Model: m.Model, Headers: m.Headers, PassthroughHeaders: m.PassthroughHeaders, TlsInsecureSkipVerify: m.TlsInsecureSkipVerify, TlsCaCertPath: m.TlsCaCertPath, TlsDisableSystemCas: m.TlsDisableSystemCas}
	switch p := m.Provider.(type) {
	case *configv1.ADKModelConfig_Openai:
		e.BaseUrl = p.Openai.BaseUrl
	case *configv1.ADKModelConfig_AzureOpenai:
		e.Endpoint = p.AzureOpenai.Endpoint
		e.Deployment = p.AzureOpenai.Deployment
		e.ApiVersion = p.AzureOpenai.ApiVersion
	case *configv1.ADKModelConfig_Anthropic:
		e.BaseUrl = p.Anthropic.BaseUrl
	case *configv1.ADKModelConfig_Mistral:
		e.BaseUrl = p.Mistral.BaseUrl
	case *configv1.ADKModelConfig_SapAiCore:
		e.BaseUrl = p.SapAiCore.BaseUrl
	case *configv1.ADKModelConfig_Foundry:
		e.Endpoint = p.Foundry.Endpoint
		e.Deployment = p.Foundry.Deployment
		e.ApiVersion = p.Foundry.ApiVersion
	}
	return e
}
