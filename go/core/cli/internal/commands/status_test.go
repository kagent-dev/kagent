package commands

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeKubectl answers kubectl invocations by their joined arguments and
// records them in order.
type fakeKubectl struct {
	outputs map[string]string
	errs    map[string]error
	calls   []string
}

func (f *fakeKubectl) run(args ...string) ([]byte, error) {
	call := strings.Join(args, " ")
	f.calls = append(f.calls, call)
	return []byte(f.outputs[call]), f.errs[call]
}

func TestStatus(t *testing.T) {
	t.Run("Given an installation in a custom namespace, when status runs, assert that it prints that namespace's pods and worker pools", func(t *testing.T) {
		kubectl := &fakeKubectl{outputs: map[string]string{
			"get pods -n team-ai":                "NAME READY\nkagent-controller 1/1\n",
			"get workerpools.ate.dev -n team-ai": "NAME READY\nkagent-default 2\n",
		}}
		var out bytes.Buffer

		require.NoError(t, runStatus(&out, "team-ai", kubectl.run))

		require.Equal(t, []string{"get pods -n team-ai", "get workerpools.ate.dev -n team-ai"}, kubectl.calls)
		require.Equal(t, "Pods in the team-ai namespace:\nNAME READY\nkagent-controller 1/1\n\nWorkerPools in the team-ai namespace:\nNAME READY\nkagent-default 2\n", out.String())
	})

	t.Run("Given a cluster that does not serve WorkerPools, when status runs, assert that it still prints the pods and returns kubectl's error", func(t *testing.T) {
		missing := errors.New("`kubectl get workerpools.ate.dev -n kagent` failed: exit status 1")
		kubectl := &fakeKubectl{
			outputs: map[string]string{
				"get pods -n kagent":                "NAME READY\nkagent-controller 1/1\n",
				"get workerpools.ate.dev -n kagent": "error: the server doesn't have a resource type \"workerpools\"\n",
			},
			errs: map[string]error{"get workerpools.ate.dev -n kagent": missing},
		}
		var out bytes.Buffer

		err := runStatus(&out, "kagent", kubectl.run)

		require.ErrorIs(t, err, missing)
		require.Contains(t, out.String(), "kagent-controller 1/1")
		require.Contains(t, out.String(), "the server doesn't have a resource type")
	})

	t.Run("Given a cluster kubectl cannot reach, when status runs, assert that it attempts both listings and returns both errors", func(t *testing.T) {
		pods, pools := errors.New("pods unreachable"), errors.New("pools unreachable")
		kubectl := &fakeKubectl{errs: map[string]error{
			"get pods -n kagent":                pods,
			"get workerpools.ate.dev -n kagent": pools,
		}}
		var out bytes.Buffer

		err := runStatus(&out, "kagent", kubectl.run)

		require.ErrorIs(t, err, pods)
		require.ErrorIs(t, err, pools)
		require.Len(t, kubectl.calls, 2)
	})
}
