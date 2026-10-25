// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"embed"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// Pin the source fixture independently of whichever revision is under test.
const (
	workspaceFixtureCommit          = "b8bbcf64174caccc8b71f158b309a00d5166fcf8"
	invalidWorkspaceAuthorization   = "Basic aW52YWxpZDppbnZhbGlk"
	anonymousWorkspaceAuthorization = "Basic Og=="
)

func workspaceFixture() *v1alpha3.WorkspaceSpec {
	return &v1alpha3.WorkspaceSpec{Git: &v1alpha3.GitWorkspaceSource{URL: "https://github.com/kagent-dev/kagent.git", Ref: workspaceFixtureCommit}}
}

//go:embed mocks/workspace.json
var workspaceMocks embed.FS

func TestSessionWorkspaceSource(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		if harness.name == "byo-adk-e2e" {
			t.Skip("BYO has no managed workspace initialization contract")
		}
		model, setPhase := startWorkspaceMock(t)
		kube := interactionKubeClient(t)
		config := harness.createModel(t, kube, reachableModelURL(t, model.URL), nil)
		template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{GenerateName: "workspace-", Namespace: "kagent", Labels: harness.labels()}, Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: config.Name}, SystemPrompt: "Run the requested workspace checks.",
		}}
		createSharedTemplate(t, kube, template)
		secret := createWorkspaceCredential(t, kube, template.Namespace)
		agent := &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Name: template.Name, Namespace: template.Namespace}, Spec: v1alpha3.AgentSpec{
			Template:   v1alpha3.AgentTemplateSource{Ref: &corev1.LocalObjectReference{Name: template.Name}},
			HarnessRef: corev1.LocalObjectReference{Name: harness.name}, Workspace: workspaceFixture(),
		}}
		agent.Spec.Workspace.Git.CredentialRef = &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name}, Key: "authorization"}
		require.NoError(t, kube.Create(t.Context(), agent))
		t.Cleanup(func() { assert.NoError(t, ctrlclient.IgnoreNotFound(kube.Delete(context.Background(), agent))) })
		key := ctrlclient.ObjectKeyFromObject(agent)
		require.Eventually(t, func() bool {
			require.NoError(t, kube.Get(t.Context(), key, agent))
			condition := meta.FindStatusCondition(agent.Status.Conditions, v1alpha3.AgentConditionReady)
			return condition != nil && condition.Status == metav1.ConditionFalse && condition.Reason == "RuntimePreparationFailed"
		}, 7*time.Minute, time.Second, "invalid Git credentials must fail preparation after the startup probe times out")
		revision, generation := agent.Status.DesiredRevision, agent.Generation
		require.NotEmpty(t, revision)
		setWorkspaceCredential(t, kube, secret, anonymousWorkspaceAuthorization)
		require.Eventually(t, func() bool {
			require.NoError(t, kube.Get(t.Context(), key, agent))
			return meta.IsStatusConditionTrue(agent.Status.Conditions, v1alpha3.AgentConditionReady)
		}, 5*time.Minute, time.Second, "correcting only the Secret must recover the failed golden")
		require.Equal(t, generation, agent.Generation)
		require.Equal(t, revision, agent.Status.LatestSuccessfulRevision)
		// Published workspaces remain usable even if credentials rotate to an
		// invalid value. New Sessions and resumes must use their prepared files.
		setWorkspaceCredential(t, kube, secret, invalidWorkspaceAuthorization)
		for instance := range 2 {
			fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
			for _, phase := range []string{"initial", "restored"} {
				setPhase(phase)
				start := len(model.Requests("", ""))
				_, _, task := fixture.send(t, "Check workspace "+phase+".")
				require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, taskText(task))
				require.Contains(t, taskText(task), "WORKSPACE_"+phase+"_DONE")
				requests := model.Requests("", "")[start:]
				found := false
				for _, request := range requests {
					last, err := lastEgressModelInput(request.Body)
					require.NoError(t, err)
					found = found || strings.Contains(string(last), "WORKSPACE_"+phase+"_OK")
				}
				require.True(t, found, "instance %d must return actual tool output for %s", instance, phase)
				if harness.name == codexE2EHarness && phase == "initial" {
					require.NotEmpty(t, requests)
					require.Contains(t, string(requests[0].Body), "Kagent Development Guide", "Codex must discover AGENTS.md before its first model request")
				}
				assertActorSuspended(t, fixture)
			}
		}
	})
}

// GitHub rejects invalid Basic credentials even for public repositories. Empty
// Basic credentials select anonymous access, exercising gateway replacement
// without requiring a private repository or a real token in the test suite.
func createWorkspaceCredential(t *testing.T, kube ctrlclient.Client, namespace string) *corev1.Secret {
	t.Helper()
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{GenerateName: "workspace-credential-", Namespace: namespace}, Data: map[string][]byte{"authorization": []byte(invalidWorkspaceAuthorization)}}
	require.NoError(t, kube.Create(t.Context(), secret))
	t.Cleanup(func() { assert.NoError(t, ctrlclient.IgnoreNotFound(kube.Delete(context.Background(), secret))) })
	return secret
}

func setWorkspaceCredential(t *testing.T, kube ctrlclient.Client, secret *corev1.Secret, value string) {
	t.Helper()
	before := secret.DeepCopy()
	secret.Data["authorization"] = []byte(value)
	require.NoError(t, kube.Patch(t.Context(), secret, ctrlclient.MergeFrom(before)))
}

func startWorkspaceMock(t *testing.T) (*modelRecorder, func(string)) {
	t.Helper()
	raw, err := workspaceMocks.ReadFile("mocks/workspace.json")
	require.NoError(t, err)
	proxies := map[string]*httputil.ReverseProxy{}
	for _, phase := range []string{"initial", "restored"} {
		command := "test \"$(pwd)\" = /data/workspace && test \"$(git rev-parse HEAD)\" = " + workspaceFixtureCommit + " && test -f AGENTS.md && "
		if phase == "initial" {
			command += "git diff --quiet && test ! -e workspace-untracked && printf edited > README.md && printf private > workspace-untracked"
		} else {
			command += "test \"$(cat README.md)\" = edited && test \"$(cat workspace-untracked)\" = private"
		}
		command += " && printf WORKSPACE_" + phase + "_OK"
		args, err := json.Marshal(struct {
			Command string `json:"command"`
		}{command})
		require.NoError(t, err)
		encodedArgs, err := json.Marshal(string(args))
		require.NoError(t, err)
		codexArgs, err := json.Marshal(struct {
			Command string `json:"cmd"`
		}{command})
		require.NoError(t, err)
		encodedCodexArgs, err := json.Marshal(string(codexArgs))
		require.NoError(t, err)
		encodedCommand, err := json.Marshal(command)
		require.NoError(t, err)
		data := strings.NewReplacer("PHASE", phase, `"COMMAND_ARGUMENTS"`, string(encodedArgs), `"CODEX_COMMAND_ARGUMENTS"`, string(encodedCodexArgs), `"COMMAND"`, string(encodedCommand)).Replace(string(raw))
		var config mockllm.Config
		require.NoError(t, json.Unmarshal([]byte(data), &config))
		upstream, err := url.Parse(startMockLLMConfig(t, config))
		require.NoError(t, err)
		proxies[phase] = httputil.NewSingleHostReverseProxy(upstream)
	}
	var active atomic.Pointer[httputil.ReverseProxy]
	active.Store(proxies["initial"])
	recorder := startModelRecorder(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { active.Load().ServeHTTP(w, r) }), nil)
	return recorder, func(phase string) { active.Store(proxies[phase]) }
}

func TestSandboxWorkspaceSource(t *testing.T) {
	t.Parallel()
	f := newSandboxFixture(t)
	kube := interactionKubeClient(t)
	template := &v1alpha3.SandboxTemplate{}
	key := ctrlclient.ObjectKey{Namespace: f.template.Namespace, Name: f.template.Name}
	require.NoError(t, kube.Get(f.ctx, key, template))
	secret := createWorkspaceCredential(t, kube, template.Namespace)
	before := template.DeepCopy()
	template.Spec.Workspace = workspaceFixture()
	template.Spec.Workspace.Git.CredentialRef = &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name}, Key: "authorization"}
	require.NoError(t, kube.Patch(f.ctx, template, ctrlclient.MergeFrom(before)))
	require.Eventually(t, func() bool {
		require.NoError(t, kube.Get(f.ctx, key, template))
		condition := meta.FindStatusCondition(template.Status.Conditions, "Ready")
		return condition != nil && condition.ObservedGeneration == template.Generation && condition.Status == metav1.ConditionFalse && condition.Reason == "PreparationFailed"
	}, 7*time.Minute, time.Second, "invalid Git credentials must fail preparation after the startup probe times out")
	generation := template.Generation
	setWorkspaceCredential(t, kube, secret, anonymousWorkspaceAuthorization)
	require.Eventually(t, func() bool {
		require.NoError(t, kube.Get(f.ctx, key, template))
		return template.Status.ObservedGeneration == template.Generation && meta.IsStatusConditionTrue(template.Status.Conditions, "Ready")
	}, 5*time.Minute, time.Second)
	require.Equal(t, generation, template.Generation)
	first := f.create(t, 10*time.Minute)
	// Keep source credentials valid while checking runtime isolation: a leaked
	// source grant must not be hidden by GitHub rejecting invalid credentials.
	command := "printf edited > README.md; printf private > workspace-untracked; if timeout 10 git ls-remote https://github.com/kagent-dev/kagent.git; then exit 1; fi"
	started, err := f.processes.StartProcess(f.guestContext(first.Id), &guestpb.StartProcessRequest{Command: []string{"sh", "-c", command}})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		process, err := f.processes.GetProcess(f.guestContext(first.Id), &guestpb.GetProcessRequest{ProcessId: started.ProcessId})
		require.NoError(t, err)
		return exitedCleanly(process)
	}, 15*time.Second, 100*time.Millisecond, "runtime must not inherit the source's Git grant")
	setWorkspaceCredential(t, kube, secret, invalidWorkspaceAuthorization)
	second := f.create(t, 10*time.Minute)
	require.Equal(t, first.PreparedRevision, second.PreparedRevision)
	for _, instance := range []*apiv1.Sandbox{first, second} {
		require.Equal(t, workspaceFixtureCommit+"\n", string(f.read(t, instance.Id, ".git/HEAD")))
	}
	_, err = f.client.SuspendSandbox(f.ctx, &apiv1.SuspendSandboxRequest{SandboxId: first.Id})
	require.NoError(t, err)
	f.wait(t, first.Id, apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED)
	_, err = f.client.ResumeSandbox(f.ctx, &apiv1.ResumeSandboxRequest{SandboxId: first.Id})
	require.NoError(t, err)
	f.wait(t, first.Id, apiv1.RuntimeState_RUNTIME_STATE_READY)
	require.Equal(t, "edited", string(f.read(t, first.Id, "README.md")))
	require.Equal(t, "private", string(f.read(t, first.Id, "workspace-untracked")))
	require.NotEqual(t, "edited", string(f.read(t, second.Id, "README.md")))
}
