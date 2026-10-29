package translator_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/pkg/config"
)

// compiledRuntime is the runtime block of the ADK contract, marshaled the way
// the compilers publish it.
func compiledRuntime(t *testing.T, defaults *configv1.RuntimeDefaultsConfig) []byte {
	t.Helper()
	data, err := runtimeconfig.Marshal(&configv1.ADKConfig{
		Runtime: translator.RuntimeConfig(defaults, "assistant", "team"),
		Agent:   &configv1.ADKAgentConfig{},
	})
	require.NoError(t, err)
	return data
}

func TestRuntimeConfigCarriesTaskStoreOnlyWhenSet(t *testing.T) {
	base := &configv1.ControllerConfig{}
	config.DefaultController(base)
	unset := compiledRuntime(t, base.GetRuntimeDefaults())
	require.NotContains(t, string(unset), "task_store")

	// An empty block must compile exactly like an absent one.
	empty := proto.CloneOf(base.GetRuntimeDefaults())
	empty.TaskStore = &configv1.TaskStoreConfig{}
	require.Nil(t, translator.RuntimeConfig(empty, "assistant", "team").GetTaskStore())
	require.True(t, bytes.Equal(unset, compiledRuntime(t, empty)), "an empty taskStore changed the compiled runtime config")

	set := proto.CloneOf(base.GetRuntimeDefaults())
	set.TaskStore = &configv1.TaskStoreConfig{OperationBudget: durationpb.New(2 * time.Minute), AttemptTimeout: durationpb.New(10 * time.Second)}
	runtime := translator.RuntimeConfig(set, "assistant", "team")
	require.True(t, proto.Equal(set.GetTaskStore(), runtime.GetTaskStore()))
	require.NotSame(t, set.GetTaskStore(), runtime.GetTaskStore(), "the compiled config must not alias controller settings")
	require.Contains(t, string(compiledRuntime(t, set)), `"task_store":{"operation_budget":"120s","attempt_timeout":"10s"}`)

	budgetOnly := proto.CloneOf(base.GetRuntimeDefaults())
	budgetOnly.TaskStore = &configv1.TaskStoreConfig{OperationBudget: durationpb.New(45 * time.Second)}
	require.Contains(t, string(compiledRuntime(t, budgetOnly)), `"task_store":{"operation_budget":"45s"}`)
}

func TestRuntimeConfigRejectsInvalidTaskStore(t *testing.T) {
	base := &configv1.ControllerConfig{}
	config.DefaultController(base)
	for name, taskStore := range map[string]*configv1.TaskStoreConfig{
		"attempt longer than budget":   {OperationBudget: durationpb.New(time.Second), AttemptTimeout: durationpb.New(2 * time.Second)},
		"budget too long":              {OperationBudget: durationpb.New(11 * time.Minute)},
		"attempt below a millisecond":  {AttemptTimeout: durationpb.New(time.Microsecond)},
		"attempt above the call limit": {AttemptTimeout: durationpb.New(31 * time.Second)},
	} {
		t.Run(name, func(t *testing.T) {
			defaults := proto.CloneOf(base.GetRuntimeDefaults())
			defaults.TaskStore = taskStore
			_, err := runtimeconfig.Marshal(&configv1.ADKConfig{
				Runtime: translator.RuntimeConfig(defaults, "assistant", "team"),
				Agent:   &configv1.ADKAgentConfig{},
			})
			require.Error(t, err)
		})
	}
}
