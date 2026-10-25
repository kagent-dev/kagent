// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"embed"
	"encoding/json"
	"fmt"
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
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// Pin the source fixture independently of whichever revision is under test.
const workspaceFixtureCommit = "b8bbcf64174caccc8b71f158b309a00d5166fcf8"

func workspaceFixture() *v1alpha3.WorkspaceSpec {
	return &v1alpha3.WorkspaceSpec{Git: &v1alpha3.GitWorkspaceSource{URL: "https://github.com/kagent-dev/kagent.git", Ref: workspaceFixtureCommit}}
}

//go:embed mocks/workspace.json
var workspaceMocks embed.FS

func TestSessionWorkspaceSource(t *testing.T) {
	target := interactionTarget(t)
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		if harness.name == "byo-adk-e2e" {
			t.Skip("BYO has no managed workspace initialization contract")
		}
		model, setPhase := startWorkspaceMock(t)
		kube := interactionKubeClient(t)
		config := harness.createModel(t, kube, reachableModelURL(t, model.URL), nil)
		template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{GenerateName: "workspace-", Namespace: "kagent", Labels: harness.labels()}, Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: config.Name}, SystemPrompt: "Run the requested workspace checks.", Workspace: workspaceFixture(),
		}}
		createAndWaitInteractionTemplate(t, harness, kube, template)
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
	f := newSandboxFixture(t)
	kube := interactionKubeClient(t)
	template := &v1alpha3.SandboxTemplate{}
	key := ctrlclient.ObjectKey{Namespace: f.template.Namespace, Name: f.template.Name}
	require.NoError(t, kube.Get(f.ctx, key, template))
	template.Spec.Workspace = workspaceFixture()
	require.NoError(t, kube.Update(f.ctx, template))
	waitPrepared := func() {
		t.Helper()
		require.Eventually(t, func() bool {
			require.NoError(t, kube.Get(f.ctx, key, template))
			return template.Status.ObservedGeneration == template.Generation && meta.IsStatusConditionTrue(template.Status.Conditions, "Ready")
		}, 5*time.Minute, time.Second)
	}
	waitPrepared()
	first, second := f.create(t, 10*time.Minute), f.create(t, 10*time.Minute)
	require.Equal(t, first.PreparedRevision, second.PreparedRevision)
	for _, instance := range []*apiv1.Sandbox{first, second} {
		require.Equal(t, workspaceFixtureCommit+"\n", string(f.read(t, instance.Id, ".git/HEAD")))
	}
	command := "printf edited > README.md; printf private > workspace-untracked; if timeout 10 git ls-remote https://github.com/kagent-dev/kagent.git; then exit 1; fi"
	started, err := f.processes.StartProcess(f.guestContext(first.Id), &guestpb.StartProcessRequest{Command: []string{"sh", "-c", command}})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		process, err := f.processes.GetProcess(f.guestContext(first.Id), &guestpb.GetProcessRequest{ProcessId: started.ProcessId})
		require.NoError(t, err)
		return exitedCleanly(process)
	}, 15*time.Second, 100*time.Millisecond, "runtime must not inherit the source's Git grant")
	_, err = f.client.SuspendSandbox(f.ctx, &apiv1.SuspendSandboxRequest{SandboxId: first.Id})
	require.NoError(t, err)
	f.wait(t, first.Id, apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED)
	_, err = f.client.ResumeSandbox(f.ctx, &apiv1.ResumeSandboxRequest{SandboxId: first.Id})
	require.NoError(t, err)
	f.wait(t, first.Id, apiv1.RuntimeState_RUNTIME_STATE_READY)
	require.Equal(t, "edited", string(f.read(t, first.Id, "README.md")))
	require.Equal(t, "private", string(f.read(t, first.Id, "workspace-untracked")))
	require.NotEqual(t, "edited", string(f.read(t, second.Id, "README.md")))

	template.Spec.Workspace.Git.RefreshToken = new("explicit-refresh")
	require.NoError(t, kube.Update(f.ctx, template))
	waitPrepared()
	refreshed := f.create(t, 5*time.Minute)
	require.NotEqual(t, first.PreparedRevision, refreshed.PreparedRevision)
	require.Equal(t, fmt.Sprintln(workspaceFixtureCommit), string(f.read(t, refreshed.Id, ".git/HEAD")))
	require.Equal(t, "edited", string(f.read(t, first.Id, "README.md")))
}
