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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
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

	t.Run("publishes only the new API group", func(t *testing.T) {
		discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
		require.NoError(t, err)
		resources, err := discoveryClient.ServerResourcesForGroupVersion("api.kagent.dev/v1alpha3")
		require.NoError(t, err)
		var names []string
		for _, resource := range resources.APIResources {
			if !strings.Contains(resource.Name, "/") {
				names = append(names, resource.Name)
			}
		}
		require.ElementsMatch(t, []string{"agents", "agenttemplates", "harnesses", "modelconfigs", "modelproviderconfigs", "remotemcpservers", "sandboxtemplates"}, names)
		_, err = discoveryClient.ServerResourcesForGroupVersion("kagent.dev/v1alpha3")
		require.True(t, apierrors.IsNotFound(err), "the new CRDs must not publish the legacy group: %v", err)
	})

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, AddToScheme(scheme))
	cl, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: scheme})
	require.NoError(t, err)

	ctx := context.Background()
	const namespace = "configuration-crd-cel"
	require.NoError(t, cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))

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
			name:   "Harness accepts Claude turn limits",
			object: validHarness(namespace, "harness-claude-limits", HarnessSpec{Claude: &ClaudeHarness{Limits: &ClaudeHarnessLimits{BudgetUSD: "2.50", MaxTurns: 40}}}),
		},
		{
			name:       "Harness Claude limits require a bound",
			object:     validHarness(namespace, "harness-claude-unbounded", HarnessSpec{Claude: &ClaudeHarness{Limits: &ClaudeHarnessLimits{}}}),
			wantReject: "at least one of budgetUSD or maxTurns must be set",
		},
		{
			name:       "Harness Claude limits reject a budget that is not a decimal",
			object:     validHarness(namespace, "harness-claude-negative-budget", HarnessSpec{Claude: &ClaudeHarness{Limits: &ClaudeHarnessLimits{BudgetUSD: "-1"}}}),
			wantReject: "spec.claude.limits.budgetUSD",
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
				Env:    []RuntimeEnvVar{{Name: "EMPTY", Value: ""}},
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
			wantReject: "exactly one of mcp or subAgent must be specified",
		},
		{
			name: "AgentTemplate tool rejects two sources",
			object: validAgentTemplate(namespace, "template-two-tools", []ToolBinding{{
				MCP: &MCPToolBinding{
					Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: "tools"},
					Tools:  []string{"search"},
				},
				SubAgent: &SubAgentToolBinding{
					Name: "helper", Description: "delegate work", TemplateRef: &corev1.LocalObjectReference{Name: "helper"},
				},
			}}),
			wantReject: "exactly one of mcp or subAgent must be specified",
		},
		{
			name: "AgentTemplate accepts a subagent template reference",
			object: validAgentTemplate(namespace, "valid-subagent", []ToolBinding{{SubAgent: &SubAgentToolBinding{
				Name: "review", Description: "Review code", TemplateRef: &corev1.LocalObjectReference{Name: "review-context"},
			}}}),
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
		{
			name: "Harness compaction requires a strategy",
			object: compactionHarness(namespace, "compaction-no-strategy", KagentHarnessCompaction{
				Summarizer: &KagentHarnessSummarizer{PromptTemplate: "Summarize.\n\n{conversation_history}"},
			}),
			wantReject: "compactionInterval or tokenThreshold must be specified",
		},
		{
			name:       "Harness compaction pairs tokenThreshold with eventRetentionSize",
			object:     compactionHarness(namespace, "compaction-threshold-alone", KagentHarnessCompaction{TokenThreshold: new(50000)}),
			wantReject: "tokenThreshold and eventRetentionSize must be specified together",
		},
		{
			name: "Harness compaction overlap requires an interval",
			object: compactionHarness(namespace, "compaction-overlap-alone", KagentHarnessCompaction{
				TokenThreshold: new(50000), EventRetentionSize: new(10), OverlapSize: new(2),
			}),
			wantReject: "overlapSize requires compactionInterval",
		},
		{
			name: "Harness summarizer prompt needs the history placeholder",
			object: compactionHarness(namespace, "compaction-prompt-without-history", KagentHarnessCompaction{
				CompactionInterval: new(5), Summarizer: &KagentHarnessSummarizer{PromptTemplate: "Summarize."},
			}),
			wantReject: "promptTemplate must contain {conversation_history}",
		},
		{
			name: "Harness rejects empty summarizer ModelConfig reference",
			object: compactionHarness(namespace, "compaction-empty-summarizer-model", KagentHarnessCompaction{
				CompactionInterval: new(5), Summarizer: &KagentHarnessSummarizer{ModelConfigRef: &corev1.LocalObjectReference{}},
			}),
			wantReject: "name must not be empty",
		},
		{
			name: "valid Harness compaction",
			object: compactionHarness(namespace, "compaction-valid", KagentHarnessCompaction{
				CompactionInterval: new(5), OverlapSize: new(2), TokenThreshold: new(50000), EventRetentionSize: new(10),
				Summarizer: &KagentHarnessSummarizer{
					ModelConfigRef: &corev1.LocalObjectReference{Name: "summarizer"}, PromptTemplate: "Summarize.\n\n{conversation_history}",
				},
			}),
		},
		{
			name:   "valid SandboxTemplate",
			object: sandboxTemplateForValidation(namespace, "valid-sandbox-template", nil),
		},
		{
			name:       "SandboxTemplate requires an immutable image",
			object:     sandboxTemplateForValidation(namespace, "sandbox-tagged-image", func(spec *SandboxTemplateSpec) { spec.Workload.Image = "example.com/guest:latest" }),
			wantReject: "spec.workload.image",
		},
		{
			name:       "SandboxTemplate requires a worker pool",
			object:     sandboxTemplateForValidation(namespace, "sandbox-empty-pool", func(spec *SandboxTemplateSpec) { spec.Substrate.WorkerPoolRef.Name = "" }),
			wantReject: "workerPoolRef name must not be empty",
		},
		{
			name:       "SandboxTemplate rejects whitespace in snapshot location",
			object:     sandboxTemplateForValidation(namespace, "sandbox-invalid-location", func(spec *SandboxTemplateSpec) { spec.Substrate.SnapshotPolicy.Location = "bad location" }),
			wantReject: "spec.substrate.snapshotPolicy.location",
		},
		{
			name:   "SandboxTemplate allows empty literal environment values",
			object: sandboxTemplateForValidation(namespace, "sandbox-empty-literal", func(spec *SandboxTemplateSpec) { spec.Env = []RuntimeEnvVar{{Name: "EMPTY", Value: ""}} }),
		},
		{
			name: "SandboxTemplate rejects duplicate environment names",
			object: sandboxTemplateForValidation(namespace, "sandbox-duplicate-env", func(spec *SandboxTemplateSpec) {
				spec.Env = []RuntimeEnvVar{{Name: "LANG", Value: ""}, {Name: "LANG", Value: ""}}
			}),
			wantReject: "Duplicate value",
		},
	}

	for _, tc := range []struct {
		name       string
		binding    SubAgentToolBinding
		wantReject string
	}{
		{name: "template-ref", binding: SubAgentToolBinding{TemplateRef: &corev1.LocalObjectReference{Name: "context"}}},
		{name: "missing-template-ref", wantReject: "templateRef: Required value"},
		{name: "empty-template-ref", binding: SubAgentToolBinding{TemplateRef: &corev1.LocalObjectReference{}}, wantReject: "templateRef.name must not be empty"},
	} {
		for _, inline := range []bool{false, true} {
			name := fmt.Sprintf("subagent-%s-inline-%t", tc.name, inline)
			t.Run(name, func(t *testing.T) {
				binding := tc.binding.DeepCopy()
				binding.Name, binding.Description = "review", "Review code"
				template := validAgentTemplate(namespace, name, []ToolBinding{{SubAgent: binding}})
				var object ctrlclient.Object = template
				if inline {
					object = &Agent{ObjectMeta: template.ObjectMeta, Spec: AgentSpec{
						Template: &template.Spec, HarnessRef: &corev1.LocalObjectReference{Name: "runner"},
					}}
				}
				err := cl.Create(ctx, object)
				if tc.wantReject != "" {
					require.ErrorContains(t, err, tc.wantReject)
					return
				}
				require.NoError(t, err)
				// Read back through the API so pruning a reference cannot masquerade as acceptance.
				require.NoError(t, cl.Get(ctx, ctrlclient.ObjectKeyFromObject(object), object))
				if agent, ok := object.(*Agent); ok {
					require.Equal(t, binding, agent.Spec.Template.Tools[0].SubAgent)
				} else {
					require.Equal(t, binding, object.(*AgentTemplate).Spec.Tools[0].SubAgent)
				}
			})
		}
	}

	for _, inlineTemplate := range []bool{false, true} {
		for _, inlineHarness := range []bool{false, true} {
			name := fmt.Sprintf("agent-%t-%t", inlineTemplate, inlineHarness)
			spec := AgentSpec{}
			if inlineTemplate {
				spec.Template = &AgentTemplateSpec{}
			} else {
				spec.TemplateRef = &corev1.LocalObjectReference{Name: "behavior"}
			}
			if inlineHarness {
				spec.Harness = &validHarness(namespace, "runner", HarnessSpec{Kagent: &KagentHarness{}}).Spec
			} else {
				spec.HarnessRef = &corev1.LocalObjectReference{Name: "runner"}
			}
			t.Run(name, func(t *testing.T) {
				agent := &Agent{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}, Spec: spec}
				require.NoError(t, cl.Create(ctx, agent))
				for _, field := range []string{"template", "harness"} {
					invalid := agent.DeepCopy()
					invalid.Name += "-both-" + field
					invalid.ResourceVersion, invalid.UID = "", ""
					if field == "template" {
						invalid.Spec.Template = &AgentTemplateSpec{}
						invalid.Spec.TemplateRef = &corev1.LocalObjectReference{Name: "behavior"}
					} else {
						invalid.Spec.Harness = &validHarness(namespace, "runner", HarnessSpec{Kagent: &KagentHarness{}}).Spec
						invalid.Spec.HarnessRef = &corev1.LocalObjectReference{Name: "runner"}
					}
					require.ErrorContains(t, cl.Create(ctx, invalid), "exactly one of "+field)
					invalid.Name = name + "-neither-" + field
					if field == "template" {
						invalid.Spec.Template = nil
						invalid.Spec.TemplateRef = nil
					} else {
						invalid.Spec.Harness = nil
						invalid.Spec.HarnessRef = nil
					}
					require.ErrorContains(t, cl.Create(ctx, invalid), "exactly one of "+field)
				}
			})
		}
	}
	for _, field := range []string{"templateRef", "harnessRef"} {
		spec := AgentSpec{TemplateRef: &corev1.LocalObjectReference{Name: "behavior"}, HarnessRef: &corev1.LocalObjectReference{Name: "runner"}}
		if field == "templateRef" {
			spec.TemplateRef.Name = ""
		} else {
			spec.HarnessRef.Name = ""
		}
		require.ErrorContains(t, cl.Create(ctx, &Agent{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "empty-" + strings.ToLower(field)}, Spec: spec}), field+".name must not be empty")
	}
	t.Run("Agent egress accepts HTTP(S) origins and leftmost-label wildcards", func(t *testing.T) {
		spec := AgentSpec{TemplateRef: &corev1.LocalObjectReference{Name: "behavior"}, HarnessRef: &corev1.LocalObjectReference{Name: "runner"},
			Egress: []string{"https://proxy.golang.org", "http://mirror.internal:8080", "https://git.internal:65535", "https://*.githubusercontent.com"}}
		require.NoError(t, cl.Create(ctx, &Agent{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "egress"}, Spec: spec}))
	})
	for i, origin := range []string{
		"proxy.golang.org", "ftp://proxy.golang.org", "https://*", "https://*.com", "https://a.*.example.com", "https://192.0.2.1", "https://[2001:db8::1]",
		"https://proxy.golang.org/path", "https://Proxy.Golang.org", "https://user@proxy.golang.org", "https://proxy.golang.org?x=1",
		"https://proxy.golang.org#f", "https://proxy.golang.org:0", "https://proxy.golang.org:70000",
	} {
		t.Run("Agent egress rejects "+origin, func(t *testing.T) {
			spec := AgentSpec{TemplateRef: &corev1.LocalObjectReference{Name: "behavior"}, HarnessRef: &corev1.LocalObjectReference{Name: "runner"}, Egress: []string{origin}}
			require.ErrorContains(t, cl.Create(ctx, &Agent{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: fmt.Sprintf("egress-invalid-%d", i)}, Spec: spec}), "spec.egress[0]")
		})
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

	t.Run("literal runtime environment", func(t *testing.T) {
		harness := validHarness(namespace, "env-harness", HarnessSpec{Kagent: &KagentHarness{}})
		for _, resource := range []struct {
			kind   string
			object ctrlclient.Object
			path   []string
		}{
			{kind: "Harness", object: harness, path: []string{"spec", "env"}},
			{kind: "SandboxTemplate", object: sandboxTemplateForValidation(namespace, "env-sandbox", nil), path: []string{"spec", "env"}},
			{kind: "Agent", object: &Agent{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "env-agent"},
				Spec: AgentSpec{
					Harness: &harness.Spec, TemplateRef: &corev1.LocalObjectReference{Name: "behavior"},
				},
			}, path: []string{"spec", "harness", "env"}},
		} {
			for _, tc := range []struct {
				name       string
				entry      map[string]any
				wantReject string
			}{
				{name: "literal", entry: map[string]any{"name": "LANG", "value": "C.UTF-8"}},
				{name: "empty", entry: map[string]any{"name": "LANG", "value": ""}},
				{name: "missing", entry: map[string]any{"name": "LANG"}, wantReject: "value: Required value"},
				{name: "null", entry: map[string]any{"name": "LANG", "value": nil}, wantReject: "value: Required value"},
				{name: "credential", entry: map[string]any{"name": "TOKEN", "credentialRef": map[string]any{"name": "auth", "key": "token"}}, wantReject: "unknown field"},
				{name: "literal-and-credential", entry: map[string]any{"name": "TOKEN", "value": "", "credentialRef": map[string]any{"name": "auth", "key": "token"}}, wantReject: "unknown field"},
			} {
				t.Run(resource.kind+"/"+tc.name, func(t *testing.T) {
					data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resource.object)
					require.NoError(t, err)
					object := &unstructured.Unstructured{Object: data}
					object.SetAPIVersion(GroupVersion.String())
					object.SetKind(resource.kind)
					object.SetName(object.GetName() + "-" + tc.name)
					require.NoError(t, unstructured.SetNestedSlice(data, []any{tc.entry}, resource.path...))
					err = cl.Create(ctx, object, &ctrlclient.CreateOptions{FieldValidation: metav1.FieldValidationStrict})
					if tc.wantReject != "" {
						require.ErrorContains(t, err, tc.wantReject)
						return
					}
					require.NoError(t, err)
					require.NoError(t, cl.Get(ctx, ctrlclient.ObjectKeyFromObject(object), object))
					entries, found, err := unstructured.NestedSlice(object.Object, resource.path...)
					require.NoError(t, err)
					require.True(t, found)
					require.Equal(t, []any{tc.entry}, entries)
				})
			}
		}
	})
}

func sandboxTemplateForValidation(namespace, name string, mutate func(*SandboxTemplateSpec)) *SandboxTemplate {
	spec := SandboxTemplateSpec{
		Workload:  SandboxTemplateWorkload{Image: "registry.example.com/guest@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Substrate: RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: RuntimeSnapshotPolicy{Location: "s3://snapshots"}},
	}
	if mutate != nil {
		mutate(&spec)
	}
	return &SandboxTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}, Spec: spec}
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

func compactionHarness(namespace, name string, compaction KagentHarnessCompaction) *Harness {
	return validHarness(namespace, name, HarnessSpec{Kagent: &KagentHarness{Compaction: &compaction}})
}
