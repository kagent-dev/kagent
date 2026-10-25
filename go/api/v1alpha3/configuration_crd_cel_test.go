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
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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
		CRDDirectoryPaths:     []string{crdChartDir(t)},
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
		require.ElementsMatch(t, []string{"agents", "agenttemplates", "egresspolicies", "harnesses", "modelconfigs", "modelproviderconfigs", "remotemcpservers", "sandboxtemplates"}, names)
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

	t.Run("MCP timeout bounds", func(t *testing.T) {
		for _, field := range []string{"timeout", "sseReadTimeout"} {
			for _, value := range []time.Duration{-time.Second, 0, 250 * time.Millisecond, 9223372036 * time.Second, 9223372036*time.Second + 1} {
				server := &RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{GenerateName: "mcp-timeout-", Namespace: namespace},
					Spec: RemoteMCPServerSpec{URL: "https://mcp.example.com"}}
				duration := &metav1.Duration{Duration: value}
				if field == "timeout" {
					server.Spec.Timeout = duration
				} else {
					server.Spec.SseReadTimeout = duration
				}
				err := cl.Create(ctx, server)
				if value <= 0 || value > 9223372036*time.Second {
					require.ErrorContains(t, err, field)
				} else {
					require.NoError(t, err)
				}
			}
		}
	})

	t.Run("optional Harness workload", func(t *testing.T) {
		for _, variant := range []struct {
			name string
			spec HarnessSpec
		}{
			{"kagent", HarnessSpec{Kagent: &KagentHarness{}}},
			{"claude", HarnessSpec{Claude: &ClaudeHarness{}}},
			{"codex", HarnessSpec{Codex: &CodexHarness{}}},
			{"byo", HarnessSpec{BYO: &BYOHarness{}}},
		} {
			for _, workload := range []string{"omitted", "empty"} {
				t.Run(variant.name+"/"+workload, func(t *testing.T) {
					harness := validHarness(namespace, "workload-"+variant.name+"-"+workload, *variant.spec.DeepCopy())
					harness.Spec.Workload = HarnessWorkload{}
					harness.SetGroupVersionKind(GroupVersion.WithKind("Harness"))
					data, err := json.Marshal(harness)
					require.NoError(t, err)
					object := &unstructured.Unstructured{}
					require.NoError(t, object.UnmarshalJSON(data))
					_, found, err := unstructured.NestedMap(object.Object, "spec", "workload")
					require.NoError(t, err)
					require.False(t, found, "zero workload must be omitted during serialization")
					if workload == "empty" {
						require.NoError(t, unstructured.SetNestedMap(object.Object, map[string]any{}, "spec", "workload"))
					}
					err = cl.Create(ctx, object)
					if variant.name == "byo" {
						require.ErrorContains(t, err, "BYO harnesses must specify workload.image")
						require.ErrorContains(t, err, "BYO harnesses must specify workload.command")
					} else {
						require.NoError(t, err)
						require.NoError(t, cl.Get(ctx, ctrlclient.ObjectKeyFromObject(object), harness))
						require.Equal(t, HarnessWorkload{}, harness.Spec.Workload)
					}
				})
			}
		}
	})

	t.Run("Harness image defaulting", func(t *testing.T) {
		for _, variant := range []struct {
			name string
			spec HarnessSpec
		}{
			{"kagent", HarnessSpec{Kagent: &KagentHarness{}}},
			{"claude", HarnessSpec{Claude: &ClaudeHarness{}}},
			{"codex", HarnessSpec{Codex: &CodexHarness{}}},
			{"byo", HarnessSpec{BYO: &BYOHarness{}, Workload: HarnessWorkload{Command: []string{"/app"}}}},
		} {
			for _, imageCase := range []struct {
				name  string
				image *string
			}{
				{"omitted", nil},
				{"empty", new("")},
				{"tagged", new("registry/runtime:latest")},
				{"pinned", new("registry/runtime@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
			} {
				t.Run(fmt.Sprintf("%s/%s", variant.name, imageCase.name), func(t *testing.T) {
					harness := validHarness(namespace, fmt.Sprintf("image-%s-%s", variant.name, imageCase.name), *variant.spec.DeepCopy())
					harness.Spec.Workload.Image = imageCase.image
					err := cl.Create(ctx, harness)
					if imageCase.name == "omitted" && variant.name == "byo" {
						require.ErrorContains(t, err, "BYO harnesses must specify workload.image")
					} else if imageCase.name == "empty" || imageCase.name == "tagged" {
						require.ErrorContains(t, err, "workload.image")
					} else {
						require.NoError(t, err)
						require.NoError(t, cl.Get(ctx, ctrlclient.ObjectKeyFromObject(harness), harness))
						require.Equal(t, imageCase.image, harness.Spec.Workload.Image)
					}
				})
			}
		}
	})

	cases := []struct {
		name       string
		object     ctrlclient.Object
		wantReject string
	}{
		{
			name:       "Harness requires one runtime",
			object:     validHarness(namespace, "harness-no-runtime", HarnessSpec{}),
			wantReject: "exactly one of the fields in [kagent codex claude byo] must be set",
		},
		{
			name: "Harness rejects multiple runtimes",
			object: validHarness(namespace, "harness-two-runtimes", HarnessSpec{
				Kagent: &KagentHarness{},
				Codex:  &CodexHarness{},
			}),
			wantReject: "exactly one of the fields in [kagent codex claude byo] must be set",
		},
		{
			name: "Harness rejects tag-only image",
			object: validHarness(namespace, "harness-tagged-image", HarnessSpec{
				Kagent:   &KagentHarness{},
				Workload: HarnessWorkload{Image: new("registry.example.com/kagent:latest")},
			}),
			wantReject: "spec.workload.image",
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
			wantReject: "exactly one of the fields in [mcp subAgent] must be set",
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
			wantReject: "exactly one of the fields in [mcp subAgent] must be set",
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
			name:       "AgentTemplate rejects outputSchemaStrict true without a schema",
			object:     unstructuredStrictTemplate(namespace, "strict-true-no-schema", true, nil, nil),
			wantReject: "outputSchemaStrict requires outputSchema or outputSchemaFrom",
		},
		{
			name:   "AgentTemplate accepts outputSchemaStrict false without a schema",
			object: unstructuredStrictTemplate(namespace, "strict-false-no-schema", false, nil, nil),
		},
		{
			name:   "AgentTemplate accepts outputSchemaStrict false with outputSchema",
			object: unstructuredStrictTemplate(namespace, "strict-false-schema", false, &apiextensionsv1.JSON{Raw: []byte(`{"type":"object"}`)}, nil),
		},
		{
			name:   "AgentTemplate accepts outputSchemaStrict true with outputSchemaFrom",
			object: unstructuredStrictTemplate(namespace, "strict-true-schema-from", true, nil, &ConfigMapKeyReference{Name: "schemas", Key: "output.json"}),
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
			wantReject: "at least one of the fields in [compactionInterval tokenThreshold] must be set",
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
						Template:   AgentTemplateSource{Inline: &template.Spec},
						HarnessRef: corev1.LocalObjectReference{Name: "runner"},
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
					require.Equal(t, binding, agent.Spec.Template.Inline.Tools[0].SubAgent)
				} else {
					require.Equal(t, binding, object.(*AgentTemplate).Spec.Tools[0].SubAgent)
				}
			})
		}
	}

	for _, inlineTemplate := range []bool{false, true} {
		name := fmt.Sprintf("agent-inline-template-%t", inlineTemplate)
		spec := AgentSpec{
			HarnessRef: corev1.LocalObjectReference{Name: "runner"},
		}
		if inlineTemplate {
			spec.Template.Inline = &AgentTemplateSpec{}
		} else {
			spec.Template.Ref = &corev1.LocalObjectReference{Name: "behavior"}
		}
		t.Run(name, func(t *testing.T) {
			agent := &Agent{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}, Spec: spec}
			require.NoError(t, cl.Create(ctx, agent))

			both := agent.DeepCopy()
			both.Name += "-both"
			both.ResourceVersion, both.UID = "", ""
			both.Spec.Template = AgentTemplateSource{
				Inline: &AgentTemplateSpec{}, Ref: &corev1.LocalObjectReference{Name: "behavior"},
			}
			require.ErrorContains(t, cl.Create(ctx, both), "exactly one of the fields in [inline ref] must be set")

			neither := agent.DeepCopy()
			neither.Name += "-neither"
			neither.ResourceVersion, neither.UID = "", ""
			neither.Spec.Template = AgentTemplateSource{}
			require.ErrorContains(t, cl.Create(ctx, neither), "exactly one of the fields in [inline ref] must be set")
		})
	}
	for _, field := range []string{"template", "harnessRef"} {
		spec := map[string]any{
			"template":   map[string]any{"ref": map[string]any{"name": "behavior"}},
			"harnessRef": map[string]any{"name": "runner"},
		}
		delete(spec, field)
		object := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": GroupVersion.String(), "kind": "Agent",
			"metadata": map[string]any{"namespace": namespace, "name": "missing-" + field},
			"spec":     spec,
		}}
		require.ErrorContains(t, cl.Create(ctx, object), field+": Required value")
	}
	for _, tc := range []struct {
		name string
		spec AgentSpec
		want string
	}{
		{
			name: "template",
			spec: AgentSpec{Template: AgentTemplateSource{Ref: &corev1.LocalObjectReference{}}, HarnessRef: corev1.LocalObjectReference{Name: "runner"}},
			want: "ref.name must not be empty",
		},
		{
			name: "harness",
			spec: AgentSpec{Template: AgentTemplateSource{Ref: &corev1.LocalObjectReference{Name: "behavior"}}},
			want: "harnessRef.name must not be empty",
		},
	} {
		require.ErrorContains(t, cl.Create(ctx, &Agent{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "empty-" + tc.name + "-ref"}, Spec: tc.spec}), tc.want)
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

	t.Run("egress policies", func(t *testing.T) { testEgressPolicyValidation(t, cl, namespace) })
	t.Run("credential headers", func(t *testing.T) { testHeaderValidation(t, cl, namespace) })
	t.Run("MCP credential headers", func(t *testing.T) { testMCPCredentialHeaderValidation(t, cl, namespace) })
	t.Run("MCP headersFrom is not served", func(t *testing.T) {
		object := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "api.kagent.dev/v1alpha3", "kind": "RemoteMCPServer",
			"metadata": map[string]any{"name": "removed-headers-from", "namespace": namespace},
			"spec":     map[string]any{"url": "https://mcp.example.com/mcp", "headersFrom": []any{map[string]any{"name": "X-Tenant", "value": "tenant"}}},
		}}
		err := cl.Create(t.Context(), object, &ctrlclient.CreateOptions{FieldValidation: metav1.FieldValidationStrict})
		require.ErrorContains(t, err, `unknown field "spec.headersFrom"`)
	})

	t.Run("literal runtime environment", func(t *testing.T) {
		harness := validHarness(namespace, "env-harness", HarnessSpec{Kagent: &KagentHarness{}})
		for _, resource := range []struct {
			kind   string
			object ctrlclient.Object
			path   []string
		}{
			{kind: "Harness", object: harness, path: []string{"spec", "env"}},
			{kind: "SandboxTemplate", object: sandboxTemplateForValidation(namespace, "env-sandbox", nil), path: []string{"spec", "env"}},
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
	if overrides.Workload.Image == nil {
		overrides.Workload.Image = new("registry.example.com/kagent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
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

func unstructuredStrictTemplate(namespace, name string, strict bool, schema *apiextensionsv1.JSON, schemaFrom *ConfigMapKeyReference) ctrlclient.Object {
	template := validAgentTemplate(namespace, name, nil)
	template.Spec.OutputSchema = schema
	template.Spec.OutputSchemaFrom = schemaFrom
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(template)
	if err != nil {
		panic(err)
	}
	object := &unstructured.Unstructured{Object: data}
	object.SetAPIVersion(GroupVersion.String())
	object.SetKind("AgentTemplate")
	if err := unstructured.SetNestedField(data, strict, "spec", "outputSchemaStrict"); err != nil {
		panic(err)
	}
	return object
}

func compactionHarness(namespace, name string, compaction KagentHarnessCompaction) *Harness {
	return validHarness(namespace, name, HarnessSpec{Kagent: &KagentHarness{Compaction: &compaction}})
}
