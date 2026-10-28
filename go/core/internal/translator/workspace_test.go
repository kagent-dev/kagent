// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package translator_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func workspaceInputs() (*v1alpha3.Agent, *v1alpha3.AgentTemplate, *v1alpha3.Harness, *corev1.Secret) {
	template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "coding", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{
		ModelConfig: &corev1.LocalObjectReference{Name: "default-model"}, SystemPrompt: "help",
	}}
	harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		Kagent:    &v1alpha3.KagentHarness{},
		Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
	}}
	agent := &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Name: "coder", Namespace: "test", UID: "uid"}, Spec: v1alpha3.AgentSpec{Template: v1alpha3.AgentTemplateSource{Ref: &corev1.LocalObjectReference{Name: template.Name}}, HarnessRef: corev1.LocalObjectReference{Name: harness.Name}}}
	agent.Spec.Workspace = &v1alpha3.WorkspaceSpec{Git: &v1alpha3.GitWorkspaceSource{URL: "https://github.com/org/repo.git", Ref: "main", CredentialRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "git"}, Key: "authorization"}}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "git", Namespace: "test"}, Data: map[string][]byte{"authorization": []byte("Bearer private-token")}}
	return agent, template, harness, secret
}

func TestWorkspaceSelectionAndPreparationAccess(t *testing.T) {
	for _, runtime := range []string{"kagent", "codex", "claude"} {
		t.Run(runtime, func(t *testing.T) {
			for _, selection := range []string{"referenced", "inline", "public", "omitted", "empty"} {
				t.Run(selection, func(t *testing.T) {
					agent, template, harness, secret := workspaceInputs()
					model := modelConfig()
					model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = "model-auth", "key"
					modelSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "model-auth", Namespace: "test"}, Data: map[string][]byte{"key": []byte("model-key")}}
					if runtime == "codex" {
						harness.Spec.Kagent = nil
						harness.Spec.Codex = &v1alpha3.CodexHarness{}
						model.Spec.OpenAI = &v1alpha3.OpenAIConfig{APIFormat: new(v1alpha3.OpenAIAPIFormatResponses)}
					}
					if runtime == "claude" {
						harness.Spec.Kagent = nil
						harness.Spec.Claude = &v1alpha3.ClaudeHarness{}
						model.Spec.Provider = v1alpha3.ModelProviderAnthropic
					}
					objects := []any{model, template, harness, modelSecret}
					wantURL := agent.Spec.Workspace.Git.URL
					switch selection {
					case "referenced", "inline":
						objects = append(objects, secret)
					case "public":
						wantURL = "https://git.example.com/other/repo.git"
						agent.Spec.Workspace = &v1alpha3.WorkspaceSpec{Git: &v1alpha3.GitWorkspaceSource{URL: wantURL, Ref: "release"}}
					case "omitted":
						wantURL = ""
						agent.Spec.Workspace = nil
					case "empty":
						wantURL = ""
						agent.Spec.Workspace = &v1alpha3.WorkspaceSpec{}
					}
					if selection == "inline" {
						agent.Spec.Template = v1alpha3.AgentTemplateSource{Inline: template.Spec.DeepCopy()}
					}
					original := agent.DeepCopy()
					result, err := compiler(t, objects...).CompileAgent(t.Context(), agent)
					require.NoError(t, err)
					require.Equal(t, original, agent)
					var compiled struct {
						Workspace *configv1.GitWorkspaceConfig `json:"workspace"`
					}
					require.NoError(t, json.Unmarshal(result.ConfigJSON, &compiled))
					require.Equal(t, wantURL, compiled.Workspace.GetUrl())
					require.NotContains(t, result.EgressDestinations, "https://github.com:443")
					for _, credential := range result.Credentials {
						require.NotEqual(t, "https://github.com:443", credential.Origin)
					}
					require.NotContains(t, string(result.ConfigJSON), "private-token")
					id, err := result.Digest()
					require.NoError(t, err)
					actor, err := substrate.ActorTemplateForRevision(&result.Revision, id)
					require.NoError(t, err)
					if selection == "empty" || selection == "omitted" {
						require.Nil(t, actor.GoldenEgressPolicy)
						require.Nil(t, compiled.Workspace)
						return
					}
					require.NotNil(t, result.GoldenAccess)
					require.NotNil(t, actor.GoldenEgressPolicy)
					require.EqualValues(t, 240, actor.Containers[0].WakeupProbe.TimeoutSeconds)
					if selection == "public" {
						require.False(t, compiled.Workspace.Authorization)
						require.Empty(t, result.GoldenAccess.Credentials)
						require.Equal(t, []string{"https://git.example.com:443"}, result.GoldenAccess.Destinations)
					} else {
						require.True(t, compiled.Workspace.Authorization)
						require.Equal(t, "ate-secret://k8s.io/default/test/git/authorization", result.GoldenAccess.Credentials[0].URI)
					}
				})
			}
		})
	}
}

func TestWorkspaceRevisionIdentity(t *testing.T) {
	agent, template, harness, secret := workspaceInputs()
	compile := func() translator.RevisionID {
		t.Helper()
		result, err := compiler(t, modelConfig(), template, harness, secret).CompileAgent(t.Context(), agent)
		require.NoError(t, err)
		id, err := result.Digest()
		require.NoError(t, err)
		return id
	}
	original := compile()
	secret.Data["authorization"] = []byte("Bearer rotated")
	require.Equal(t, original, compile(), "Secret values are resolved only at the gateway")
	agent.Generation++
	agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: "live"}}
	require.Equal(t, original, compile(), "live policy edits and incidental generations do not change the prepared revision")
	agent.Spec.Workspace.Git.Ref = "0123456789abcdef0123456789abcdef01234567"
	pinned := compile()
	require.NotEqual(t, original, pinned)
	agent.Spec.Workspace.Git.CredentialRef.Key = "other"
	secret.Data["other"] = secret.Data["authorization"]
	require.NotEqual(t, pinned, compile(), "the reference, not its value, is immutable input")
}

func TestAgentsChooseRepositoriesIndependentlyOfTemplate(t *testing.T) {
	first, template, harness, secret := workspaceInputs()
	child := template.DeepCopy()
	child.Name = "reviewer"
	template.Spec.Tools = []v1alpha3.ToolBinding{{SubAgent: &v1alpha3.SubAgentToolBinding{Name: child.Name, Description: "review", TemplateRef: &corev1.LocalObjectReference{Name: child.Name}}}}
	second := first.DeepCopy()
	second.Name = "other-repo"
	second.Spec.Workspace = &v1alpha3.WorkspaceSpec{Git: &v1alpha3.GitWorkspaceSource{URL: "https://git.example.com/other/repo.git", Ref: "release"}}
	original := template.DeepCopy()
	compiler := compiler(t, modelConfig(), template, child, harness, secret)
	for _, agent := range []*v1alpha3.Agent{first, second} {
		result, err := compiler.CompileAgent(t.Context(), agent)
		require.NoError(t, err)
		var compiled struct {
			Workspace *configv1.GitWorkspaceConfig `json:"workspace"`
			Agent     *configv1.ADKAgentConfig     `json:"agent"`
		}
		require.NoError(t, json.Unmarshal(result.ConfigJSON, &compiled))
		require.Equal(t, agent.Spec.Workspace.Git.URL, compiled.Workspace.Url)
		require.Equal(t, agent.Spec.Workspace.Git.Ref, compiled.Workspace.Ref)
		require.Equal(t, agent.Spec.Workspace.Git.CredentialRef != nil, compiled.Workspace.Authorization)
		require.Len(t, compiled.Agent.SubAgents, 1, "shared subagents use the root Agent's workspace")
	}
	require.Equal(t, original, template)
}

func TestWorkspaceRejectsUnsupportedAndMissingInputs(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*v1alpha3.Agent, *v1alpha3.Harness, *corev1.Secret)
		want   string
	}{
		{"missing credential", func(_ *v1alpha3.Agent, _ *v1alpha3.Harness, s *corev1.Secret) { s.Data = nil }, "missing or empty"},
		{"optional credential", func(a *v1alpha3.Agent, _ *v1alpha3.Harness, _ *corev1.Secret) {
			a.Spec.Workspace.Git.CredentialRef.Optional = new(true)
		}, "cannot be optional"},
		{"IP URL", func(a *v1alpha3.Agent, _ *v1alpha3.Harness, _ *corev1.Secret) {
			a.Spec.Workspace.Git.URL = "https://127.0.0.1/repo.git"
		}, "DNS"},
		{"BYO", func(_ *v1alpha3.Agent, h *v1alpha3.Harness, _ *corev1.Secret) {
			h.Spec.Kagent = nil
			h.Spec.BYO = &v1alpha3.BYOHarness{}
		}, "do not support workspace"},
	} {
		t.Run(test.name, func(t *testing.T) {
			agent, template, harness, secret := workspaceInputs()
			test.mutate(agent, harness, secret)
			_, err := compiler(t, modelConfig(), template, harness, secret).CompileAgent(t.Context(), agent)
			require.ErrorContains(t, err, test.want)
		})
	}
}
