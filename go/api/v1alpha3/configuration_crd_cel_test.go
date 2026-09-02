/*
Copyright 2026.

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
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestConfigurationCRDValidation(t *testing.T) {
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
	cl, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: scheme})
	require.NoError(t, err)

	ctx := context.Background()
	const namespace = "configuration-crd-cel"
	require.NoError(t, cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))

	empty := ""
	cases := []struct {
		name       string
		object     ctrlclient.Object
		wantReject string
	}{
		{
			name:       "Harness requires one runtime",
			object:     validHarness(namespace, "harness-no-runtime", HarnessSpec{}),
			wantReject: "exactly one of kagent, codex, claude, or byo must be specified",
		},
		{
			name: "Harness rejects multiple runtimes",
			object: validHarness(namespace, "harness-two-runtimes", HarnessSpec{
				Kagent: &KagentHarness{},
				Codex:  &CodexHarness{},
			}),
			wantReject: "exactly one of kagent, codex, claude, or byo must be specified",
		},
		{
			name: "Harness rejects tag-only image",
			object: validHarness(namespace, "harness-tagged-image", HarnessSpec{
				Kagent:   &KagentHarness{},
				Workload: HarnessWorkload{Image: "registry.example.com/kagent:latest"},
			}),
			wantReject: "spec.workload.image",
		},
		{
			name: "Harness env requires a value source",
			object: validHarness(namespace, "harness-empty-env", HarnessSpec{
				Kagent: &KagentHarness{},
				Env:    []HarnessEnvVar{{Name: "EMPTY"}},
			}),
			wantReject: "exactly one of value or credentialRef must be specified",
		},
		{
			name: "Harness env rejects two value sources",
			object: validHarness(namespace, "harness-two-env-sources", HarnessSpec{
				Kagent: &KagentHarness{},
				Env: []HarnessEnvVar{{
					Name:          "MODEL_KEY",
					Value:         &empty,
					CredentialRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model"}, Key: "key"},
				}},
			}),
			wantReject: "exactly one of value or credentialRef must be specified",
		},
		{
			name: "Harness memory requires a model reference",
			object: validHarness(namespace, "harness-empty-memory-model", HarnessSpec{
				Kagent: &KagentHarness{Memory: &KagentHarnessMemory{}},
			}),
			wantReject: "modelConfigRef name must not be empty",
		},
		{
			name: "valid kagent memory Harness",
			object: validHarness(namespace, "valid-memory-harness", HarnessSpec{
				Kagent: &KagentHarness{Memory: &KagentHarnessMemory{
					ModelConfigRef: corev1.LocalObjectReference{Name: "embedding-model"}, TTLDays: 7,
				}},
			}),
		},
		{
			name: "valid Harness",
			object: validHarness(namespace, "valid-harness", HarnessSpec{
				Claude: &ClaudeHarness{},
				Env:    []HarnessEnvVar{{Name: "EMPTY", Value: &empty}},
			}),
		},
		{
			name: "valid BYO Harness",
			object: validHarness(namespace, "valid-byo-harness", HarnessSpec{
				BYO:      &BYOHarness{},
				Workload: HarnessWorkload{Command: []string{"/agent"}},
			}),
		},
		{
			name: "BYO Harness requires workload command",
			object: validHarness(namespace, "byo-missing-command", HarnessSpec{
				BYO: &BYOHarness{},
			}),
			wantReject: "BYO harnesses must specify workload.command",
		},
		{
			name:       "AgentTemplate tool requires one source",
			object:     validAgentTemplate(namespace, "template-empty-tool", []ToolBinding{{}}),
			wantReject: "exactly one of mcp or agent must be specified",
		},
		{
			name: "AgentTemplate tool rejects two sources",
			object: validAgentTemplate(namespace, "template-two-tools", []ToolBinding{{
				MCP: &MCPToolBinding{
					Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: "tools"},
					Tools:  []string{"search"},
				},
				Agent: &AgentToolBinding{
					Name: "helper", Description: "delegate work", TemplateRef: corev1.LocalObjectReference{Name: "helper"},
				},
			}}),
			wantReject: "exactly one of mcp or agent must be specified",
		},
		{
			name:   "valid AgentTemplate",
			object: validAgentTemplate(namespace, "valid-template", nil),
		},
		{
			name:   "AgentTemplate permits omitted ModelConfig",
			object: &AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "model-free-template", Namespace: namespace}},
		},
		{
			name: "AgentTemplate rejects empty ModelConfig reference",
			object: &AgentTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "empty-model-reference", Namespace: namespace},
				Spec:       AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{}},
			},
			wantReject: "name must not be empty",
		},
		{
			name: "AgentTemplate rejects unsupported MCP server kind",
			object: validAgentTemplate(namespace, "unsupported-mcp-kind", []ToolBinding{{
				MCP: &MCPToolBinding{Server: corev1.TypedLocalObjectReference{Kind: "Service", Name: "tools"}},
			}}),
			wantReject: "kind must be RemoteMCPServer",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := cl.Create(ctx, tc.object)
			if tc.wantReject == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantReject)
		})
	}
}

func validHarness(namespace, name string, overrides HarnessSpec) *Harness {
	if overrides.Workload.Image == "" {
		overrides.Workload.Image = "registry.example.com/kagent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	}
	if overrides.Substrate.WorkerPoolRef.Name == "" {
		overrides.Substrate.WorkerPoolRef.Name = "default"
	}
	if overrides.Substrate.SnapshotPolicy.Location == "" {
		overrides.Substrate.SnapshotPolicy.Location = "gs://snapshots/kagent"
	}
	return &Harness{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Spec: overrides}
}

func validAgentTemplate(namespace, name string, tools []ToolBinding) *AgentTemplate {
	return &AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: "default"},
			Tools:       tools,
		},
	}
}
