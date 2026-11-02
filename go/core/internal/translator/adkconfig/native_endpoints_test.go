package adkconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestGeminiEndpointTranslation(t *testing.T) {
	for _, custom := range []bool{false, true} {
		for _, auth := range []string{"none", "secret", "actorJwt"} {
			t.Run(auth, func(t *testing.T) {
				baseURL := ""
				origin := "https://generativelanguage.googleapis.com:443"
				if custom {
					baseURL = "https://models.example.com/team/v1"
					origin = "https://models.example.com:443"
				}
				spec := v1alpha3.ModelConfigSpec{
					Provider: v1alpha3.ModelProviderGemini, Model: "catalog-alias",
					Gemini:         &v1alpha3.GeminiConfig{},
					DefaultHeaders: map[string]string{"x-route": "team"}, TLS: &v1alpha3.TLSConfig{DisableVerify: true},
				}
				if custom {
					spec.Gemini.BaseURL = &baseURL
				}
				switch auth {
				case "secret":
					spec.APIKeySecret, spec.APIKeySecretKey = "model-auth", "token"
				case "actorJwt":
					spec.ActorJWT = &v1alpha3.ActorJWT{Audiences: []string{"model-service"}}
				}
				resolved := &translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "native"}, Spec: spec}}
				m, data, err := translateModel(resolved)
				require.NoError(t, err)
				require.Equal(t, spec.Model, m.Model)
				require.Equal(t, "team", m.Headers["x-route"])
				header, prefix := "x-goog-api-key", ""
				if auth == "actorJwt" {
					header, prefix = "authorization", "Bearer "
				}
				if auth != "none" {
					require.Equal(t, prefix+translator.CredentialPlaceholder, m.Headers[header])
					require.Len(t, m.Headers, 2)
				} else {
					require.Equal(t, spec.DefaultHeaders, m.Headers)
				}
				require.Equal(t, map[string]string{"x-route": "team"}, spec.DefaultHeaders)
				require.Equal(t, new(true), m.TlsInsecureSkipVerify)
				require.False(t, m.ApiKeyPassthrough)
				raw, err := runtimeconfig.Marshal(&configv1.ADKConfig{Agent: &configv1.ADKAgentConfig{Model: m}})
				require.NoError(t, err)
				var decoded configv1.ADKConfig
				require.NoError(t, runtimeconfig.Unmarshal(raw, &decoded))
				require.Equal(t, baseURL, decoded.Agent.Model.GetGemini().BaseUrl)
				require.Equal(t, []string{origin}, agentConfigDestinations(&configv1.ADKAgentConfig{}, resolved.Config, m))
				input := &translator.HarnessInput{Harness: &translator.HarnessConfiguration{}, Root: &translator.AgentInput{Template: &translator.TemplateConfiguration{}, ResolvedModelConfig: resolved}}
				environment, credentials, targets, err := translator.CompileCredentials(input, nil, data.EnvVars)
				require.NoError(t, err)
				require.Empty(t, targets)
				for _, variable := range environment {
					require.Nil(t, variable.ValueFrom)
				}
				if auth != "none" {
					require.Len(t, credentials, 1)
					require.Equal(t, origin, credentials[0].Origin)
					require.Equal(t, header, credentials[0].Header)
					require.Equal(t, prefix, credentials[0].Prefix)
					if auth == "actorJwt" {
						require.Empty(t, credentials[0].URI)
						require.Equal(t, []string{"model-service"}, credentials[0].ActorJWT.Audiences)
					}
					if auth == "secret" {
						require.Contains(t, credentials[0].URI, "/model-auth/token")
					}
				} else {
					require.Empty(t, environment)
					require.Empty(t, credentials)
				}
			})
		}
	}
}
