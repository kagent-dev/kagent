/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha3

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestOpenAIConfigValidation pins the OpenAIConfig admission rules against a
// real kube-apiserver loaded with the shipped CRDs:
//   - maxTokens and maxCompletionTokens are mutually exclusive (type-level
//     AtMostOneOf), because native OpenAI reasoning models reject max_tokens
//     while some OpenAI-compatible endpoints reject max_completion_tokens, so
//     sending both risks hard 400s.
//   - both fields carry Minimum=1, so a non-positive value is rejected at
//     admission rather than silently ignored by the translator.
//   - reasoningEffort accepts the provider-wide superset, including xhigh and
//     minimal, while rejecting values outside that set.
func TestOpenAIConfigValidation(t *testing.T) {
	testEnv := &envtest.Environment{
		BinaryAssetsDirectory: envtestAssetsDir(t),
		CRDDirectoryPaths:     []string{crdChartDir(t)},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { _ = testEnv.Stop() })

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, AddToScheme(scheme))
	cl, err := ctrl_client.New(cfg, ctrl_client.Options{Scheme: scheme})
	require.NoError(t, err)

	ctx := context.Background()
	const ns = "openai-cel"
	require.NoError(t, cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}))

	cases := []struct {
		name       string
		build      func() ctrl_client.Object
		wantReject string // substring in admission error; empty means accept
	}{
		{
			name: "both maxTokens and maxCompletionTokens rejected",
			build: func() ctrl_client.Object {
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-both-token-caps", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "gpt-4",
						Provider: ModelProviderOpenAI,
						OpenAI:   &OpenAIConfig{MaxTokens: 1000, MaxCompletionTokens: 1000},
					},
				}
			},
			wantReject: "at most one of the fields in [maxTokens maxCompletionTokens] may be set",
		},
		{
			name: "maxTokens below minimum rejected",
			build: func() ctrl_client.Object {
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-maxtokens-neg", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "gpt-4",
						Provider: ModelProviderOpenAI,
						OpenAI:   &OpenAIConfig{MaxTokens: -1},
					},
				}
			},
			wantReject: "maxTokens",
		},
		{
			name: "maxCompletionTokens below minimum rejected",
			build: func() ctrl_client.Object {
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-maxcompletion-neg", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "gpt-4",
						Provider: ModelProviderOpenAI,
						OpenAI:   &OpenAIConfig{MaxCompletionTokens: -1},
					},
				}
			},
			wantReject: "maxCompletionTokens",
		},
		{
			name: "only maxTokens accepted",
			build: func() ctrl_client.Object {
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-maxtokens-only", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "gpt-4",
						Provider: ModelProviderOpenAI,
						OpenAI:   &OpenAIConfig{MaxTokens: 1000},
					},
				}
			},
		},
		{
			name: "only maxCompletionTokens accepted",
			build: func() ctrl_client.Object {
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-maxcompletion-only", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "gpt-5",
						Provider: ModelProviderOpenAI,
						OpenAI:   &OpenAIConfig{MaxCompletionTokens: 1000},
					},
				}
			},
		},
		{
			name: "neither token cap accepted",
			build: func() ctrl_client.Object {
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-no-token-caps", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "gpt-4",
						Provider: ModelProviderOpenAI,
						OpenAI:   &OpenAIConfig{},
					},
				}
			},
		},
		{
			name: "xhigh reasoning effort accepted",
			build: func() ctrl_client.Object {
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-reasoning-xhigh", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "gpt-5.6-luna",
						Provider: ModelProviderOpenAI,
						OpenAI:   &OpenAIConfig{ReasoningEffort: new(OpenAIReasoningEffort("xhigh"))},
					},
				}
			},
		},
		{
			name: "minimal reasoning effort remains accepted",
			build: func() ctrl_client.Object {
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-reasoning-minimal", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "compatible-reasoning-model",
						Provider: ModelProviderOpenAI,
						OpenAI:   &OpenAIConfig{ReasoningEffort: new(OpenAIReasoningEffort("minimal"))},
					},
				}
			},
		},
		{
			name: "unknown reasoning effort rejected",
			build: func() ctrl_client.Object {
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-reasoning-unknown", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "gpt-5.6-luna",
						Provider: ModelProviderOpenAI,
						OpenAI:   &OpenAIConfig{ReasoningEffort: new(OpenAIReasoningEffort("extreme"))},
					},
				}
			},
			wantReject: "reasoningEffort",
		},
		{
			name: "mistral config with matching provider accepted",
			build: func() ctrl_client.Object {
				baseURL := "https://api.mistral.ai/v1"
				temp := "0.3"
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-mistral-ok", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "mistral-large-latest",
						Provider: ModelProviderMistral,

						Mistral: &MistralConfig{
							BaseURL:     &baseURL,
							Temperature: &temp,
						}, Headers: []Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "mistral-secret"}, Key: "api-key"}}}},
					},
				}
			},
		},
		{
			name: "mistral config without any provider-specific block accepted",
			build: func() ctrl_client.Object {
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-mistral-plain", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "mistral-medium-latest",
						Provider: ModelProviderMistral, Headers: []Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "mistral-secret"}, Key: "api-key"}}}},
					},
				}
			},
		},
		{
			name: "mistral config with mismatched provider rejected",
			build: func() ctrl_client.Object {
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-mistral-bad-provider", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "mistral-large-latest",
						Provider: ModelProviderOpenAI,

						Mistral: &MistralConfig{}, Headers: []Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "openai-secret"}, Key: "api-key"}}}},
					},
				}
			},
			wantReject: "provider.mistral must be nil if the provider is not Mistral",
		},
		{
			name: "mistral maxTokens below minimum rejected",
			build: func() ctrl_client.Object {
				neg := -1
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-mistral-maxtokens-neg", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "mistral-large-latest",
						Provider: ModelProviderMistral,

						Mistral: &MistralConfig{MaxTokens: &neg}, Headers: []Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "mistral-secret"}, Key: "api-key"}}}},
					},
				}
			},
			wantReject: "maxTokens",
		},
		{
			name: "azure apiVersion omitted on chat completions rejected",
			build: func() ctrl_client.Object {
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-azure-no-version", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:       "gpt-4.1-mini",
						Provider:    ModelProviderAzureOpenAI,
						AzureOpenAI: &AzureOpenAIConfig{Endpoint: "https://team.openai.azure.com"},
					},
				}
			},
			wantReject: "apiVersion is required unless apiFormat is responses",
		},
		{
			name: "azure apiVersion omitted on responses accepted",
			build: func() ctrl_client.Object {
				responses := OpenAIAPIFormatResponses
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-azure-responses", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:       "gpt-4.1-mini",
						Provider:    ModelProviderAzureOpenAI,
						AzureOpenAI: &AzureOpenAIConfig{Endpoint: "https://team.openai.azure.com", APIFormat: &responses},
					},
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := cl.Create(ctx, c.build())
			if c.wantReject == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), c.wantReject)
		})
	}

	for _, field := range []string{"n", "timeout", "azure-max-tokens"} {
		for _, value := range []int{-1, 0, 1} {
			t.Run(fmt.Sprintf("%s=%d", field, value), func(t *testing.T) {
				model := &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{GenerateName: "numeric-", Namespace: ns},
					Spec:       ModelConfigSpec{Model: "gpt-4", Provider: ModelProviderOpenAI, OpenAI: &OpenAIConfig{}},
				}
				switch field {
				case "n":
					model.Spec.OpenAI.N = &value
				case "timeout":
					model.Spec.OpenAI.Timeout = &value
				case "azure-max-tokens":
					model.Spec.Provider, model.Spec.OpenAI = ModelProviderAzureOpenAI, nil
					model.Spec.AzureOpenAI = &AzureOpenAIConfig{Endpoint: "https://team.openai.azure.com", APIVersion: "2024-10-21", MaxTokens: &value}
				}
				err := cl.Create(ctx, model)
				if value <= 0 {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			})
		}
	}

	for _, raw := range []string{`{"thinking":{"budget_tokens":1024},"large":9007199254740993}`, `[]`, `1`} {
		model := &ModelConfig{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "bedrock-json-", Namespace: ns},
			Spec: ModelConfigSpec{Provider: ModelProviderBedrock, Model: "claude", Bedrock: &BedrockConfig{
				Region: "us-east-1", AdditionalModelRequestFields: &apiextensionsv1.JSON{Raw: []byte(raw)},
			}},
		}
		err := cl.Create(ctx, model)
		if raw[0] != '{' {
			require.ErrorContains(t, err, "additionalModelRequestFields")
		} else {
			require.NoError(t, err)
			stored := &ModelConfig{}
			require.NoError(t, cl.Get(ctx, ctrl_client.ObjectKeyFromObject(model), stored))
			require.Contains(t, string(stored.Spec.Bedrock.AdditionalModelRequestFields.Raw), `"large":9007199254740993`)
			require.Contains(t, string(stored.Spec.Bedrock.AdditionalModelRequestFields.Raw), `"budget_tokens":1024`)
		}
	}

	for _, tc := range []struct {
		name   string
		stream *bool
		want   bool
	}{
		{name: "default", want: true},
		{name: "enabled", stream: new(true), want: true},
		{name: "disabled", stream: new(false), want: false},
	} {
		t.Run("stream "+tc.name, func(t *testing.T) {
			model := &ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "mc-stream-" + tc.name, Namespace: ns},
				Spec:       ModelConfigSpec{Model: "gpt-4", Provider: ModelProviderOpenAI, Stream: tc.stream},
			}
			require.NoError(t, cl.Create(ctx, model))
			stored := &ModelConfig{}
			require.NoError(t, cl.Get(ctx, ctrl_client.ObjectKeyFromObject(model), stored))
			require.Equal(t, new(tc.want), stored.Spec.Stream)
		})
	}
}
