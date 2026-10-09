package e2e_test

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:embed mocks/invoke_bash_tool.json
var bashToolMocks []byte

const (
	// slowRepositoryPrefix serves the skill repository with every request
	// held for slowRepositoryDelay, so one fetch, two or three requests, takes
	// longer than the bash tool's former fixed 30 s limit while each request
	// stays short of any proxy's idle timeout.
	slowRepositoryPrefix = "/slow"
	slowRepositoryDelay  = 16 * time.Second
	oldBashToolTimeout   = 30 * time.Second
)

// bashToolFixture is a kagent Harness Agent with the bash tool: its skill comes
// from a git host on the test runner, whose slow path the bash tool fetches
// through the Session's egress.
type bashToolFixture struct {
	templateFor func(t *testing.T, harnessName string) string
	model       *modelRecorder
}

func newBashToolFixture(t *testing.T) *bashToolFixture {
	t.Helper()
	kube := interactionKubeClient(t)
	repositories := newGitFixture(t, "")
	unthrottled := repositories.handler
	repositories.handler = http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if path, ok := strings.CutPrefix(request.URL.Path, slowRepositoryPrefix+"/"); ok {
			select {
			case <-time.After(slowRepositoryDelay):
			case <-request.Context().Done():
				return
			}
			request.URL.Path = "/" + path
		}
		unthrottled.ServeHTTP(w, request)
	})
	server := repositories.serveThroughEgress(t)

	var cfg mockllm.Config
	raw := bytes.ReplaceAll(bashToolMocks, []byte("SLOW_REPOSITORY_URL"), []byte(server+slowRepositoryPrefix+"/"+gitSkillRepository))
	require.NoError(t, json.Unmarshal(raw, &cfg))
	model := startModelRecorder(t, startMockLLMConfig(t, cfg), nil)
	modelConfig := createInteractionModel(t, kube, reachableModelURL(t, model.URL), nil)

	return &bashToolFixture{
		model: model,
		templateFor: func(t *testing.T, harnessName string) string {
			t.Helper()
			template := &v1alpha3.AgentTemplate{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "bash-tool-", Namespace: "kagent",
					Labels: map[string]string{"kagent.dev/e2e-runtime": goADKHarness.runtimeLabel, "kagent.dev/harness": harnessName},
				},
				Spec: v1alpha3.AgentTemplateSpec{
					ModelConfig:  &corev1.LocalObjectReference{Name: modelConfig.Name},
					SystemPrompt: "Use the bash tool.",
					// A skill is what gives the kagent Harness its bash tool.
					Skills: []v1alpha3.AgentTemplateSkill{{Name: gitFixtureSkill, Source: v1alpha3.ArtifactSource{
						Git:  &v1alpha3.GitArtifact{URL: server + "/" + gitSkillRepository, Commit: repositories.commit},
						Path: "skills/" + gitFixtureSkill,
					}}},
				},
			}
			createAndWaitInteractionTemplateForHarness(t, kube, template, harnessName)
			return template.Name
		},
	}
}

// requireReply fails with the last tool result the model saw unless the turn
// completed with want.
func (f *bashToolFixture) requireReply(t *testing.T, task *a2atype.Task, want string) {
	t.Helper()
	if task.Status.State == a2atype.TaskStateCompleted && strings.Contains(taskText(task), want) {
		return
	}
	var last string
	if requests := f.model.Requests("", ""); len(requests) > 0 {
		last = string(requests[len(requests)-1].Body)
		if len(last) > 4096 {
			last = last[len(last)-4096:]
		}
	}
	t.Fatalf("task %s with %q, want %q; last model request: %s", task.Status.State, taskText(task), want, last)
}

// A process the bash tool leaves in the background ends with the turn, so the
// next turn, which may run for another caller, finds nothing of it.
func TestBashToolEndsBackgroundJobsWithTheTurn(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	fixture := newBashToolFixture(t)
	session := newInteractionFixtureForTemplate(t, goADKHarness, target, fixture.templateFor(t, goADKHarness.name))

	_, _, task := session.send(t, "Start a background sleep.")
	fixture.requireReply(t, task, "BACKGROUND_SLEEP_STARTED")

	_, _, task = session.send(t, "Count the sleeps.")
	fixture.requireReply(t, task, "NO_SLEEP_LEFT")
}

// A git fetch longer than the former fixed 30 s limit completes under the
// default timeout; KAGENT_BASH_TOOL_TIMEOUT=2s on the Harness restores a short
// limit.
func TestBashToolTimeoutFitsGit(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	fixture := newBashToolFixture(t)
	shortTimeout := cloneHarness(t, kube, goADKHarness.name, "bash-timeout-", func(harness *v1alpha3.Harness) {
		harness.Spec.Env = append(harness.Spec.Env, v1alpha3.RuntimeEnvVar{Name: "KAGENT_BASH_TOOL_TIMEOUT", Value: "2s"})
	})

	t.Run("default timeout", func(t *testing.T) {
		t.Parallel()
		session := newInteractionFixtureForTemplate(t, goADKHarness, target, fixture.templateFor(t, goADKHarness.name))
		start := time.Now()
		_, _, task := session.send(t, "Fetch the slow repository.")
		elapsed := time.Since(start)
		fixture.requireReply(t, task, "FETCH_COMPLETED")
		require.Greater(t, elapsed, oldBashToolTimeout, "the fetch was not slower than the former limit")
	})
	t.Run("KAGENT_BASH_TOOL_TIMEOUT=2s", func(t *testing.T) {
		t.Parallel()
		session := newInteractionFixtureForTemplate(t, goADKHarness, target, fixture.templateFor(t, shortTimeout.Name))
		_, _, task := session.send(t, "Fetch the slow repository.")
		fixture.requireReply(t, task, "FETCH_TIMED_OUT")
	})
}
