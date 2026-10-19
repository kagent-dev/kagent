package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/google/uuid"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

// Every harness executes this same foreground skill through its normal shell
// tool. The test changes policy while the invocation is waiting for the probe.
const egressProbeScript = `#!/bin/sh
control=$1
target=$2
run=$3
echo "EGRESS_PROBE_STARTED_$run"
for i in $(seq 1 900); do
  phase=$(wget -T 5 -qO- "$control/phase") || exit 1
  if [ "$phase" = stop ]; then
    echo "EGRESS_PROBE_FINISHED_$run"
    wget -T 5 -qO- "$control/stopped" >/dev/null
    exit
  fi
  if wget -T 3 -qO- --header 'Authorization: Bearer kagent-credential-injected' --header 'X-API-Key: kagent-credential-injected' "$target/?phase=$phase"; then
    result=ALLOWED
  else
    result=DENIED
  fi
  echo "EGRESS_RESULT_${phase}_$result"
  wget -T 5 -qO- "$control/result?phase=$phase&result=$result" >/dev/null || exit 1
  sleep 0.2
done
exit 1
`

func TestSessionEgressPolicyUpdatesLiveActors(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	image := publishSkillImage(t)
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		kube := interactionKubeClient(t)
		destination, requests := startEgressDestination(t)
		modelURL, model, runs := startEgressSkillMock(t, harness.skillsRoot(), destination, "live", "checkpoint", "fresh", "resumed", "fork")
		config := harness.createModel(t, kube, modelURL, nil)
		template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{GenerateName: "live-egress-", Namespace: "kagent", Labels: harness.labels()}, Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: config.Name}, SystemPrompt: "Load the egress skill and run its probe.",
			Skills: []v1alpha3.AgentTemplateSkill{{Name: "egress", Source: v1alpha3.ArtifactSource{OCI: image, Path: "egress"}}},
		}}
		createAndWaitInteractionTemplate(t, harness, kube, template)
		fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
		actor, err := findSubstrateActor(fixture.ctx, fixture.system, "kagent", substrate.ActorName(fixture.sessionID))
		require.NoError(t, err)
		actorUID := actor.GetMetadata().GetUid()
		agent := &v1alpha3.Agent{}
		key := ctrlclient.ObjectKey{Namespace: "kagent", Name: template.Name}
		require.NoError(t, kube.Get(t.Context(), key, agent))
		revision := agent.Status.LatestSuccessfulRevision
		grant := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{GenerateName: "live-access-", Namespace: "kagent"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: destination}}}}
		require.NoError(t, kube.Create(t.Context(), grant))
		t.Cleanup(func() { require.NoError(t, ctrlclient.IgnoreNotFound(kube.Delete(context.Background(), grant))) })
		setGrant := func(origin string) {
			t.Helper()
			require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(grant), grant))
			grant.Spec.Destinations[0].Origin = origin
			require.NoError(t, kube.Update(t.Context(), grant))
		}

		run := runs("live")
		finish := runEgressSkill(t, fixture, model, run, "initial_DENIED", "allowed_ALLOWED", "conflict_ALLOWED", "fixed_ALLOWED", "revoked_DENIED")
		run.expect(t, "initial", "DENIED")
		require.Zero(t, requests("live_initial"), "ungranted requests reached the destination")
		require.NoError(t, kube.Get(t.Context(), key, agent))
		agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: grant.Name}}
		require.NoError(t, kube.Update(t.Context(), agent))
		run.setPhase("allowed")
		run.expect(t, "allowed", "ALLOWED")
		require.Positive(t, requests("live_allowed"))
		waitEgressCondition := func(reason string) {
			t.Helper()
			require.NoError(t, wait.PollUntilContextTimeout(t.Context(), time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
				if err := kube.Get(ctx, key, agent); err != nil {
					return false, err
				}
				condition := apimeta.FindStatusCondition(agent.Status.Conditions, v1alpha3.AgentConditionEgressPoliciesApplied)
				return condition != nil && condition.Reason == reason &&
					condition.ObservedGeneration == agent.Generation &&
					apimeta.IsStatusConditionTrue(agent.Status.Conditions, v1alpha3.AgentConditionReady), nil
			}))
			require.Equal(t, revision, agent.Status.LatestSuccessfulRevision)
		}
		waitEgressCondition(v1alpha3.EgressPoliciesReasonApplied)
		// The opposite protocol on the model's exact host/port conflicts with
		// pinned access. Existing grants must keep working in this invocation.
		modelOrigin, err := url.Parse(modelURL)
		require.NoError(t, err)
		require.Equal(t, "http", modelOrigin.Scheme)
		require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(grant), grant))
		grant.Spec.Destinations = append(grant.Spec.Destinations, v1alpha3.EgressDestination{Origin: "https://" + modelOrigin.Host})
		require.NoError(t, kube.Update(t.Context(), grant))
		waitEgressCondition(v1alpha3.EgressPoliciesReasonConflict)
		require.True(t, apimeta.IsStatusConditionFalse(agent.Status.Conditions, v1alpha3.AgentConditionEgressPoliciesApplied))
		run.setPhase("conflict")
		run.expect(t, "conflict", "ALLOWED")
		require.Positive(t, requests("live_conflict"))
		require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(grant), grant))
		grant.Spec.Destinations = grant.Spec.Destinations[:1]
		require.NoError(t, kube.Update(t.Context(), grant))
		waitEgressCondition(v1alpha3.EgressPoliciesReasonApplied)
		require.True(t, apimeta.IsStatusConditionTrue(agent.Status.Conditions, v1alpha3.AgentConditionEgressPoliciesApplied))
		run.setPhase("fixed")
		run.expect(t, "fixed", "ALLOWED")

		setGrant("https://unused.example.com")
		run.setPhase("revoked")
		run.expect(t, "revoked", "DENIED")
		revokedCount := requests("live_revoked")
		require.Never(t, func() bool { return requests("live_revoked") != revokedCount }, time.Second, 100*time.Millisecond)
		actor, err = findSubstrateActor(fixture.ctx, fixture.system, "kagent", substrate.ActorName(fixture.sessionID))
		require.NoError(t, err)
		require.Equal(t, actorUID, actor.GetMetadata().GetUid(), "live updates must not replace the actor")
		require.Equal(t, ateapipb.ActorState_ACTOR_STATE_RUNNING, actor.GetStatus().GetState())
		finish()
		assertActorSuspended(t, fixture)

		// Save a checkpoint while the source is granted access, then revoke it while
		// suspended. Neither resume nor a DATA fork may resurrect that old grant.
		setGrant(destination)
		run = runs("checkpoint")
		finish = runEgressSkill(t, fixture, model, run, "initial_ALLOWED")
		run.expect(t, "initial", "ALLOWED")
		task := finish()
		assertActorSuspended(t, fixture)
		checkpoint := createCheckpoint(t, fixture.ctx, fixture.checkpoints, &apiv1.CreateCheckpointRequest{SessionId: fixture.sessionID, RequestId: uuid.NewString(), ExpectedHeadTaskId: string(task.ID)}).GetCheckpoint()
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(fixture.ctx), time.Minute)
			defer cancel()
			_, err := fixture.checkpoints.DeleteCheckpoint(ctx, &apiv1.DeleteCheckpointRequest{CheckpointId: checkpoint.Id})
			require.NoError(t, err)
		})
		require.NoError(t, kube.Delete(t.Context(), grant))
		require.NoError(t, wait.PollUntilContextTimeout(t.Context(), time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
			if err := kube.Get(ctx, key, agent); err != nil {
				return false, err
			}
			condition := apimeta.FindStatusCondition(agent.Status.Conditions, v1alpha3.AgentConditionEgressPoliciesApplied)
			return condition != nil && condition.Reason == v1alpha3.EgressPoliciesReasonNotFound && apimeta.IsStatusConditionTrue(agent.Status.Conditions, v1alpha3.AgentConditionReady), nil
		}))
		require.Equal(t, revision, agent.Status.LatestSuccessfulRevision, "policy edits must not replace the runtime revision")
		created, err := fixture.sessions.CreateSession(fixture.ctx, &apiv1.CreateSessionRequest{Agent: &apiv1.ResourceReference{Namespace: key.Namespace, Name: key.Name}, RequestId: uuid.NewString()})
		require.NoError(t, err, "unresolved policy must not block the last successful runtime")
		require.Equal(t, revision, created.Session.PreparedRevision)
		fresh := *fixture
		fresh.sessionID, fresh.contextID = created.Session.Id, created.Session.ContextId
		t.Cleanup(func() {
			require.NoError(t, deleteIdleSession(context.WithoutCancel(fresh.ctx), fresh.sessions, fresh.sessionID))
		})
		run = runs("fresh")
		finish = runEgressSkill(t, &fresh, model, run, "initial_DENIED")
		run.expect(t, "initial", "DENIED")
		require.Zero(t, requests("fresh_initial"))
		finish()
		run = runs("resumed")
		finish = runEgressSkill(t, fixture, model, run, "initial_DENIED")
		run.expect(t, "initial", "DENIED")
		require.Zero(t, requests("resumed_initial"))
		finish()
		assertActorSuspended(t, fixture)
		actor, err = findSubstrateActor(fixture.ctx, fixture.system, "kagent", substrate.ActorName(fixture.sessionID))
		require.NoError(t, err)
		require.Equal(t, actorUID, actor.GetMetadata().GetUid())
		forked, err := fixture.checkpoints.ForkSession(fixture.ctx, &apiv1.ForkSessionRequest{CheckpointId: checkpoint.Id, RequestId: uuid.NewString()})
		require.NoError(t, err)
		fork := *fixture
		fork.sessionID = forked.Session.Id
		fork.contextID = forked.Session.ContextId
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(fork.ctx), time.Minute)
			defer cancel()
			require.NoError(t, deleteIdleSession(ctx, fork.sessions, fork.sessionID))
		})
		run = runs("fork")
		finish = runEgressSkill(t, &fork, model, run, "initial_DENIED")
		run.expect(t, "initial", "DENIED")
		require.Zero(t, requests("fork_initial"))
		finish()
	})
}

type egressProbeRun struct {
	name                  string
	proxy                 *httputil.ReverseProxy
	mu                    sync.Mutex
	phase                 string
	lastPhase, lastResult string
	consecutive           int
	stopped               chan struct{}
	stopOnce              sync.Once
	completed             <-chan egressInvocationResult
}

type egressInvocationResult struct {
	message *a2apb.SendMessageResponse
	err     error
}

func (e *egressProbeRun) setPhase(phase string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.phase = e.name + "_" + phase
}
func (e *egressProbeRun) expect(t *testing.T, phase, result string) {
	t.Helper()
	err := wait.PollUntilContextTimeout(t.Context(), 100*time.Millisecond, 90*time.Second, true, func(context.Context) (bool, error) {
		select {
		case invocation := <-e.completed:
			if invocation.err != nil {
				return false, fmt.Errorf("egress invocation failed: %w", invocation.err)
			}
			return false, fmt.Errorf("egress invocation ended before the probe: %v", invocation.message.GetTask().GetStatus())
		default:
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.lastPhase == e.name+"_"+phase && e.lastResult == result && e.consecutive >= 3, nil
	})
	require.NoError(t, err, "%s %s did not become %s", e.name, phase, result)
}
func (e *egressProbeRun) finish(ctx context.Context) error {
	e.mu.Lock()
	e.phase = "stop"
	e.mu.Unlock()
	select {
	case <-e.stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func runEgressSkill(t *testing.T, fixture *interactionFixture, model *modelRecorder, run *egressProbeRun, expectedResults ...string) func() *a2atype.Task {
	t.Helper()
	start := len(model.Requests("", ""))
	_, request := newMessageRequest(t, "Run egress skill "+run.name+".")
	request.Message.ContextId = fixture.sessionID
	request.Tenant = fixture.tenant
	done := make(chan egressInvocationResult, 1)
	run.completed = done
	ctx, cancel := context.WithCancel(fixture.ctx)
	t.Cleanup(func() {
		cancel()
		if t.Failed() {
			for _, request := range model.Requests("", "")[start:] {
				last, err := lastEgressModelInput(request.Body)
				if err == nil && len(last) != 0 {
					t.Logf("egress %s model input: %s", run.name, last)
				}
			}
		}
	})
	go func() {
		r, err := sendMessageWithRetry(ctx, fixture.client, request)
		done <- egressInvocationResult{r, err}
	}()
	return func() *a2atype.Task {
		t.Helper()
		stopCtx, stop := context.WithTimeout(ctx, 15*time.Second)
		defer stop()
		require.NoError(t, run.finish(stopCtx))
		var result egressInvocationResult
		select {
		case result = <-done:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		require.NoError(t, result.err)
		parsed, err := pbconv.FromProtoSendMessageResponse(result.message)
		require.NoError(t, err)
		task, ok := parsed.(*a2atype.Task)
		require.True(t, ok)
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, taskText(task))
		require.Contains(t, taskText(task), "EGRESS_"+run.name+"_DONE")
		// Check actual tool output in fresh model inputs, not the model's fixed text
		// or conversation history copied into a resumed session or checkpoint fork.
		var outputs []byte
		for _, request := range model.Requests("", "")[start:] {
			last, err := lastEgressModelInput(request.Body)
			require.NoError(t, err)
			outputs = append(outputs, last...)
		}
		for _, marker := range []string{"SKILL_BODY_EGRESS", "EGRESS_PROBE_STARTED_" + run.name, "EGRESS_PROBE_FINISHED_" + run.name} {
			require.Contains(t, string(outputs), marker)
		}
		for _, result := range expectedResults {
			require.Contains(t, string(outputs), "EGRESS_RESULT_"+run.name+"_"+result)
		}
		return task
	}
}

func lastEgressModelInput(raw []byte) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var body struct {
		Messages []json.RawMessage `json:"messages"`
		Input    []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	messages := append(body.Messages, body.Input...)
	if len(messages) == 0 {
		return nil, nil
	}
	return messages[len(messages)-1], nil
}

func startEgressDestination(t *testing.T) (string, func(string) int) {
	t.Helper()
	var mu sync.Mutex
	requests := map[string]int{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Query().Get("phase")]++
		mu.Unlock()
		_, _ = io.WriteString(w, "EGRESS_DESTINATION_REACHED\n")
	}))
	_ = server.Listener.Close()
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	destination, err := url.Parse(reachableServerURL(t, server.URL, ""))
	require.NoError(t, err)
	// Keep a distinct origin even on Docker Desktop, where host mocks otherwise
	// share host.docker.internal with the implicitly allowed model endpoint.
	kube := interactionKubeClient(t)
	alias := &corev1.Service{ObjectMeta: metav1.ObjectMeta{GenerateName: "egress-origin-", Namespace: "kagent"}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: destination.Hostname()}}
	require.NoError(t, kube.Create(t.Context(), alias))
	t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), alias)) })
	destination.Host = net.JoinHostPort(alias.Name+".kagent.svc.cluster.local", destination.Port())
	return destination.String(), func(phase string) int { mu.Lock(); defer mu.Unlock(); return requests[phase] }
}

func startEgressSkillMock(t *testing.T, root, destination string, runNames ...string) (string, *modelRecorder, func(string) *egressProbeRun) {
	t.Helper()
	require.NotEmpty(t, runNames)
	var active atomic.Pointer[egressProbeRun]
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		run := active.Load()
		if run == nil {
			http.Error(w, "no active probe", http.StatusServiceUnavailable)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/egress/") {
			if !strings.HasPrefix(r.URL.Path, "/egress/"+run.name+"/") {
				http.Error(w, "old probe", http.StatusGone)
				return
			}
			switch {
			case strings.HasSuffix(r.URL.Path, "/phase"):
				run.mu.Lock()
				phase := run.phase
				run.mu.Unlock()
				_, _ = io.WriteString(w, phase)
			case strings.HasSuffix(r.URL.Path, "/result"):
				phase, result := r.URL.Query().Get("phase"), r.URL.Query().Get("result")
				run.mu.Lock()
				if phase == run.lastPhase && result == run.lastResult {
					run.consecutive++
				} else {
					run.lastPhase, run.lastResult, run.consecutive = phase, result, 1
				}
				run.mu.Unlock()
			case strings.HasSuffix(r.URL.Path, "/stopped"):
				run.stopOnce.Do(func() { close(run.stopped) })
			default:
				http.NotFound(w, r)
			}
			return
		}
		run.proxy.ServeHTTP(w, r)
	})
	recorder := startModelRecorder(t, upstream, nil)
	modelURL := reachableModelURL(t, recorder.URL)
	origin := strings.TrimSuffix(modelURL, "/v1")
	raw, err := interactionMocks.ReadFile("mocks/invoke_egress_skill.json")
	require.NoError(t, err)
	runs := map[string]*egressProbeRun{}
	for _, phase := range runNames {
		command := fmt.Sprintf("sh %s/egress/probe.sh '%s/egress/%s' '%s' %s", root, origin, phase, destination, phase)
		replacements := []string{"PHASE", phase, "ROOT", root}
		args, err := json.Marshal(struct {
			Command string `json:"command"`
		}{command})
		require.NoError(t, err)
		encodedArgs, err := json.Marshal(string(args))
		require.NoError(t, err)
		codexArgs, err := json.Marshal(struct {
			Command     string `json:"cmd"`
			YieldTimeMS int    `json:"yield_time_ms"`
		}{command, 30000})
		require.NoError(t, err)
		encodedCodexArgs, err := json.Marshal(string(codexArgs))
		require.NoError(t, err)
		encodedCommand, err := json.Marshal(command)
		require.NoError(t, err)
		replacements = append(replacements, `"COMMAND_ARGUMENTS"`, string(encodedArgs), `"CODEX_COMMAND_ARGUMENTS"`, string(encodedCodexArgs), `"COMMAND"`, string(encodedCommand))
		var config mockllm.Config
		require.NoError(t, json.Unmarshal([]byte(strings.NewReplacer(replacements...).Replace(string(raw))), &config))
		upstream, err := url.Parse(startMockLLMConfig(t, config))
		require.NoError(t, err)
		run := &egressProbeRun{name: phase, proxy: httputil.NewSingleHostReverseProxy(upstream), stopped: make(chan struct{})}
		run.setPhase("initial")
		runs[phase] = run
	}
	active.Store(runs[runNames[0]])
	return modelURL, recorder, func(name string) *egressProbeRun {
		t.Helper()
		run := runs[name]
		require.NotNil(t, run, "unregistered egress probe %q", name)
		active.Store(run)
		return run
	}
}

func TestSandboxEgressPolicyUpdatesLiveActor(t *testing.T) {
	t.Parallel()
	f := newSandboxFixture(t)
	kube := interactionKubeClient(t)
	destination, requests := startEgressDestination(t)
	instance := f.create(t, 5*time.Minute)
	actor, err := findSubstrateActor(f.ctx, f.system, f.template.Namespace, substrate.ActorName(instance.Id))
	require.NoError(t, err)
	actorUID := actor.GetMetadata().GetUid()
	probe := func(phase string) string {
		t.Helper()
		command := fmt.Sprintf("if wget -T 3 -qO- '%s/?phase=%s'; then printf ALLOWED; else printf DENIED; fi", destination, phase)
		process, err := f.processes.StartProcess(f.guestContext(instance.Id), &guestpb.StartProcessRequest{Command: []string{"sh", "-c", command}})
		require.NoError(t, err)
		outputs, err := f.processes.StreamProcessOutputs(f.guestContext(instance.Id), &guestpb.StreamProcessOutputsRequest{ProcessId: process.ProcessId, Follow: true})
		require.NoError(t, err)
		var result strings.Builder
		for {
			chunk, err := outputs.Recv()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			if chunk.Source == guestpb.OutputSource_OUTPUT_SOURCE_STDOUT {
				result.Write(chunk.Data)
			}
		}
		return result.String()
	}
	require.Contains(t, probe("initial"), "DENIED")
	require.Zero(t, requests("initial"))
	grant := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{GenerateName: "sandbox-access-", Namespace: f.template.Namespace}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: destination}}}}
	require.NoError(t, kube.Create(f.ctx, grant))
	t.Cleanup(func() { require.NoError(t, ctrlclient.IgnoreNotFound(kube.Delete(context.Background(), grant))) })
	template := &v1alpha3.SandboxTemplate{}
	key := ctrlclient.ObjectKey{Namespace: f.template.Namespace, Name: f.template.Name}
	require.NoError(t, kube.Get(f.ctx, key, template))
	template.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: grant.Name}}
	require.NoError(t, kube.Update(f.ctx, template))
	require.Eventually(t, func() bool { return strings.Contains(probe("allowed"), "ALLOWED") }, time.Minute, time.Second)
	require.Positive(t, requests("allowed"))
	require.NoError(t, kube.Delete(f.ctx, grant))
	require.Eventually(t, func() bool { return strings.Contains(probe("revoked"), "DENIED") }, time.Minute, time.Second)
	require.NoError(t, wait.PollUntilContextTimeout(f.ctx, time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, key, template); err != nil {
			return false, err
		}
		condition := apimeta.FindStatusCondition(template.Status.Conditions, v1alpha3.SandboxTemplateConditionEgressPoliciesApplied)
		return condition != nil && condition.Reason == v1alpha3.EgressPoliciesReasonNotFound && apimeta.IsStatusConditionTrue(template.Status.Conditions, "Ready"), nil
	}))
	original := instance
	instance = f.create(t, 5*time.Minute)
	require.Equal(t, original.PreparedRevision, instance.PreparedRevision)
	require.Contains(t, probe("fresh"), "DENIED")
	require.Zero(t, requests("fresh"))
	instance = original
	_, err = f.client.SuspendSandbox(f.ctx, &apiv1.SuspendSandboxRequest{SandboxId: instance.Id})
	require.NoError(t, err)
	_, err = f.client.ResumeSandbox(f.ctx, &apiv1.ResumeSandboxRequest{SandboxId: instance.Id})
	require.NoError(t, err)
	require.Contains(t, probe("resumed"), "DENIED")
	require.Zero(t, requests("resumed"))
	current, err := f.client.GetSandbox(f.ctx, &apiv1.GetSandboxRequest{SandboxId: instance.Id})
	require.NoError(t, err)
	require.Equal(t, instance.PreparedRevision, current.Sandbox.PreparedRevision)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.Sandbox.State)
	actor, err = findSubstrateActor(f.ctx, f.system, f.template.Namespace, substrate.ActorName(instance.Id))
	require.NoError(t, err)
	require.Equal(t, actorUID, actor.GetMetadata().GetUid())
}

func TestSessionEgressDeniesUnconfiguredDestination(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		target := interactionTarget(t)
		var reachedDenied atomic.Bool
		origin := startModelRecorder(t, mockLLMProxy(t, startMockLLMServer(t, interactionMocks, "mocks/invoke_agent.json")), func([]byte) error {
			reachedDenied.Store(true)
			return nil
		})
		denied, err := url.Parse(reachableModelURL(t, origin.URL))
		require.NoError(t, err)
		// Give the destination a distinct hostname even when host mocks use
		// host.docker.internal. The gateway allowlist is keyed by hostname.
		kube := interactionKubeClient(t)
		alias := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "denied-origin-", Namespace: "kagent"},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: denied.Hostname()},
		}
		require.NoError(t, kube.Create(t.Context(), alias))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), alias)) })
		denied.Host = net.JoinHostPort(alias.Name+".kagent.svc.cluster.local", denied.Port())

		// First prove the destination is reachable and speaks this harness's
		// model protocol when explicitly configured.
		control := newInteractionFixture(t, harness, target, denied.String())
		_, _, task := control.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
		require.Contains(t, taskText(task), "The answer is 4.")
		require.True(t, reachedDenied.Load())
		reachedDenied.Store(false)

		var modelRequests atomic.Int32
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Claude also sends a HEAD probe during startup.
			if r.Method == http.MethodPost {
				modelRequests.Add(1)
			}
			// The model host is allowed, but its redirect must not expand the
			// Actor's policy to another reachable, unconfigured destination.
			redirect := *denied
			redirect.Path, redirect.RawQuery = r.URL.Path, r.URL.RawQuery
			http.Redirect(w, r, redirect.String(), http.StatusTemporaryRedirect)
		}))
		_ = server.Listener.Close()
		listener, err := net.Listen("tcp", "0.0.0.0:0")
		if err != nil {
			t.Fatal(err)
		}
		server.Listener = listener
		server.Start()
		t.Cleanup(server.Close)
		fixture := newInteractionFixture(t, harness, target, reachableModelURL(t, server.URL))
		// TODO: Revisit this deadline and the one-request assertion when native
		// retries are restored after https://github.com/agent-substrate/substrate/issues/2663.
		// A timeout or cancellation must not count as a successful denial.
		ctx, cancel := context.WithTimeout(fixture.ctx, 30*time.Second)
		t.Cleanup(cancel)
		fixture.ctx = ctx
		t.Cleanup(func() {
			t.Logf("redirected model requests = %d, denied destination reached = %v", modelRequests.Load(), reachedDenied.Load())
		})
		_, _, task = fixture.send(t, "What is 2+2?")
		require.Equal(t, int32(1), modelRequests.Load(), "expected one model request without retries")
		require.Equal(t, a2atype.TaskStateFailed, task.Status.State, "redirect task text = %q", taskText(task))
		require.False(t, reachedDenied.Load(), "redirect reached an unconfigured destination")
	})
}
