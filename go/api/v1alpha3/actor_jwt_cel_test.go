package v1alpha3

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func testActorJWTValidation(t *testing.T, cl ctrlclient.Client, namespace string) {
	t.Run("credential union", func(t *testing.T) { testCredentialUnionValidation(t, cl, namespace) })
	for _, kind := range []string{"ModelConfig", "RemoteMCPServer", "EgressPolicy"} {
		for _, tc := range []struct {
			name   string
			token  ActorJWT
			reject bool
		}{
			{"default lifetime", ActorJWT{Audiences: []string{"service"}}, false},
			{"explicit lifetime", ActorJWT{Audiences: []string{"service", "kagent-agent-info"}, ExpirationSeconds: new(int64(600))}, false},
			{"no audiences", ActorJWT{}, true},
			{"empty audience", ActorJWT{Audiences: []string{""}}, true},
			{"reserved audience", ActorJWT{Audiences: []string{"service", "kagent-runtime"}}, true},
			{"duplicate audience", ActorJWT{Audiences: []string{"service", "service"}}, true},
			{"short lifetime", ActorJWT{Audiences: []string{"service"}, ExpirationSeconds: new(int64(299))}, true},
			{"long lifetime", ActorJWT{Audiences: []string{"service"}, ExpirationSeconds: new(int64(3601))}, true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				metadata := metav1.ObjectMeta{GenerateName: "actor-jwt-", Namespace: namespace}
				var object ctrlclient.Object
				switch kind {
				case "ModelConfig":
					object = &ModelConfig{ObjectMeta: metadata, Spec: ModelConfigSpec{Provider: ModelProviderOpenAI, Model: "test", ActorJWT: &tc.token}}
				case "RemoteMCPServer":
					object = &RemoteMCPServer{ObjectMeta: metadata, Spec: RemoteMCPServerSpec{URL: "https://mcp.example.com", ActorJWT: &tc.token}}
				case "EgressPolicy":
					object = &EgressPolicy{ObjectMeta: metadata, Spec: EgressPolicySpec{Destinations: []EgressDestination{{Origin: "https://api.example.com", Authentication: &EgressAuthentication{Header: "Authorization", Prefix: new("Bearer "), ActorJWT: &tc.token}}}}}
				}
				err := cl.Create(t.Context(), object)
				if tc.reject {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				require.NoError(t, cl.Get(t.Context(), ctrlclient.ObjectKeyFromObject(object), object))
				var token *ActorJWT
				switch obj := object.(type) {
				case *ModelConfig:
					token = obj.Spec.ActorJWT
				case *RemoteMCPServer:
					token = obj.Spec.ActorJWT
				case *EgressPolicy:
					token = obj.Spec.Destinations[0].Authentication.ActorJWT
				}
				expected := int64(900)
				if tc.token.ExpirationSeconds != nil {
					expected = *tc.token.ExpirationSeconds
				}
				require.Equal(t, expected, *token.ExpirationSeconds)
			})
		}
	}
	token := &ActorJWT{Audiences: []string{"service"}}
	for _, spec := range []ModelConfigSpec{
		{Provider: ModelProviderGemini},
		{Provider: ModelProviderOllama, Ollama: &OllamaConfig{}},
		{Provider: ModelProviderOllama, Ollama: &OllamaConfig{Host: "https://gateway.example.com"}},
	} {
		t.Run(string(spec.Provider)+" rejects Actor JWT", func(t *testing.T) {
			spec.Model, spec.ActorJWT = "test:cloud", token
			model := &ModelConfig{ObjectMeta: metav1.ObjectMeta{GenerateName: "actor-jwt-provider-", Namespace: namespace}, Spec: spec}
			require.ErrorContains(t, cl.Create(t.Context(), model), "actorJwt requires a provider with a configurable endpoint and header credentials")
		})
	}
	t.Run("Bedrock requires AWS credentials", func(t *testing.T) {
		model := &ModelConfig{ObjectMeta: metav1.ObjectMeta{GenerateName: "actor-jwt-bedrock-", Namespace: namespace}, Spec: ModelConfigSpec{
			Provider: ModelProviderBedrock, Model: "test", Bedrock: &BedrockConfig{Region: "us-east-1"}, ActorJWT: token,
		}}
		require.ErrorContains(t, cl.Create(t.Context(), model), "Bedrock requires AWS credentials and does not support actorJwt")
	})
	for _, object := range []ctrlclient.Object{
		&ModelConfig{Spec: ModelConfigSpec{Provider: ModelProviderOpenAI, Model: "test", ActorJWT: token, TokenPassthrough: new(true)}},
		&ModelConfig{Spec: ModelConfigSpec{Provider: ModelProviderOpenAI, Model: "test", ActorJWT: token, APIKeySecret: "auth", APIKeySecretKey: "key"}},
		&ModelConfig{Spec: ModelConfigSpec{Provider: ModelProviderGeminiVertexAI, Model: "test", ActorJWT: token}},
		&RemoteMCPServer{Spec: RemoteMCPServerSpec{URL: "https://mcp.example.com", ActorJWT: token, TokenPassthrough: new(true)}},
		&RemoteMCPServer{Spec: RemoteMCPServerSpec{URL: "https://mcp.example.com", ActorJWT: token, HeadersFrom: []ValueRef{{Name: "aUTHORIZATION", Value: "conflict"}}}},
		&EgressPolicy{Spec: EgressPolicySpec{Destinations: []EgressDestination{{Origin: "https://api.example.com", Authentication: &EgressAuthentication{Header: "Authorization", ActorJWT: token, SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}, Key: "key"}}}}}},
	} {
		object.SetNamespace(namespace)
		object.SetGenerateName("actor-jwt-conflict-")
		require.Error(t, cl.Create(t.Context(), object))
	}
}

func testCredentialUnionValidation(t *testing.T, cl ctrlclient.Client, namespace string) {
	for _, kind := range []string{"ModelConfig", "RemoteMCPServer"} {
		for _, tc := range []struct {
			name        string
			actor       bool
			secret      bool
			passthrough *bool
			reject      bool
		}{
			{name: "none"},
			{name: "actor", actor: true},
			{name: "secret", secret: true},
			{name: "passthrough", passthrough: new(true)},
			{name: "disabled passthrough", passthrough: new(false)},
			{name: "actor and secret", actor: true, secret: true, reject: true},
			{name: "actor and passthrough", actor: true, passthrough: new(true), reject: true},
			{name: "actor and disabled passthrough", actor: true, passthrough: new(false), reject: true},
			{name: "secret and passthrough", secret: true, passthrough: new(true), reject: true},
			{name: "secret and disabled passthrough", secret: true, passthrough: new(false), reject: true},
			{name: "all three", actor: true, secret: true, passthrough: new(true), reject: true},
		} {
			if kind == "RemoteMCPServer" && tc.secret {
				continue // MCP Secrets use headersFrom rather than apiKeySecret.
			}
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				metadata := metav1.ObjectMeta{GenerateName: "credential-union-", Namespace: namespace}
				var token *ActorJWT
				if tc.actor {
					token = &ActorJWT{Audiences: []string{"service"}}
				}
				var object ctrlclient.Object
				if kind == "ModelConfig" {
					model := &ModelConfig{ObjectMeta: metadata, Spec: ModelConfigSpec{Model: "test", ActorJWT: token, TokenPassthrough: tc.passthrough}}
					if tc.secret {
						model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = "auth", "key"
					}
					object = model
				} else {
					object = &RemoteMCPServer{ObjectMeta: metadata, Spec: RemoteMCPServerSpec{URL: "https://mcp.example.com", ActorJWT: token, TokenPassthrough: tc.passthrough}}
				}
				err := cl.Create(t.Context(), object)
				if tc.reject {
					require.True(t, apierrors.IsInvalid(err), "expected admission rejection, got %v", err)
					require.ErrorContains(t, err, "at most one of the fields")
				} else {
					require.NoError(t, err)
				}
			})
		}
	}
}
