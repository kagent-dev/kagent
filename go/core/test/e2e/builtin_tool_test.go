package e2e_test

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
)

func TestE2EBuiltinToolInConfigSecret(t *testing.T) {
	// Setup mock server
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_inline_agent.json")
	defer stopServer()

	// Setup Kubernetes client
	cli := setupK8sClient(t, false)

	// Setup specific resources
	modelCfg := setupModelConfig(t, cli, baseURL)

	// Define tools
	tools := []*v1alpha2.Tool{
		{
			Type: v1alpha2.ToolProviderType_Builtin,
			Builtin: &v1alpha2.BuiltinTool{
				ToolNames: []string{"ask_user"},
			},
		},
	}

	agent := setupAgent(t, cli, modelCfg.Name, tools)

	secret := &corev1.Secret{}
	err := cli.Get(t.Context(), client.ObjectKey{
		Namespace: agent.Namespace,
		Name:      agent.Name,
	}, secret)
	require.NoError(t, err, "Should be able to read agent config Secret")

	configJSON, ok := secret.Data["config.json"]
	require.True(t, ok, "Secret should contain config.json key")

	var configMap map[string]json.RawMessage
	err = json.Unmarshal(configJSON, &configMap)
	require.NoError(t, err, "Should unmarshal config.json")

	builtinToolsRaw, ok := configMap["builtin_tools"]
	require.True(t, ok, "config.json should contain builtin_tools key, got keys: %v", mapKeys(configMap))

	var builtinTools []string
	err = json.Unmarshal(builtinToolsRaw, &builtinTools)
	require.NoError(t, err, "Should unmarshal builtin_tools as []string")
	require.Contains(t, builtinTools, "ask_user",
		"builtin_tools should contain ask_user, got: %v", builtinTools)
}

func TestE2ENoBuiltinToolWhenAbsent(t *testing.T) {
	// Setup mock server
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_inline_agent.json")
	defer stopServer()

	// Setup Kubernetes client
	cli := setupK8sClient(t, false)

	// Setup specific resources
	modelCfg := setupModelConfig(t, cli, baseURL)

	agent := setupAgent(t, cli, modelCfg.Name, nil)

	secret := &corev1.Secret{}
	err := cli.Get(t.Context(), client.ObjectKey{
		Namespace: agent.Namespace,
		Name:      agent.Name,
	}, secret)
	require.NoError(t, err, "Should be able to read agent config Secret")

	configJSON, ok := secret.Data["config.json"]
	require.True(t, ok, "Secret should contain config.json key")

	var configMap map[string]json.RawMessage
	err = json.Unmarshal(configJSON, &configMap)
	require.NoError(t, err, "Should unmarshal config.json")

	_, ok = configMap["builtin_tools"]
	require.False(t, ok,
		"config.json should NOT contain builtin_tools when no Builtin tools are specified")
}

func mapKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
