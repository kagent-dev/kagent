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

package v1alpha2

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestOpenAIConfigCELValidation pins the OpenAIConfig admission rules against a
// real kube-apiserver loaded with the shipped CRDs:
//   - maxTokens and maxCompletionTokens are mutually exclusive (type-level
//     XValidation), because native OpenAI reasoning models reject max_tokens
//     while some OpenAI-compatible endpoints reject max_completion_tokens, so
//     sending both risks hard 400s.
//   - both fields carry Minimum=1, so a non-positive value is rejected at
//     admission rather than silently ignored by the translator.
func TestOpenAIConfigCELValidation(t *testing.T) {
	testEnv := &envtest.Environment{
		BinaryAssetsDirectory: envtestAssetsDir(t),
		CRDDirectoryPaths:     []string{crdBasesDir(t)},
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
			wantReject: "maxTokens and maxCompletionTokens are mutually exclusive",
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
}
