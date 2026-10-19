package driver

import (
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

func TestRunPreparesForTheTurnsActorJWTHosts(t *testing.T) {
	refused := errors.New("refused")
	var got []string
	driver := NewProcessDriver(ProcessConfig{Prepare: func(actorJWTHosts []string) error {
		got = actorJWTHosts
		return refused
	}})
	ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{
		adk.ActorJWTHostsHeader: {"Muster.Example.svc"},
	}))

	_, err := driver.Run(ctx, runtime.Turn{Prompt: "list agents"}, nil)
	require.ErrorIs(t, err, refused, "omp does not start on files the turn could not write")
	require.Equal(t, []string{"muster.example.svc"}, got)
}
