package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/kagent-dev/kagent/go/core/internal/anthropicselfhostedsandboxworker"
)

func TestLoadEnvironmentSandboxTemplates(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config.yaml")
	require.NoError(t, os.WriteFile(config, []byte(`environments:
  - environmentSecretRef: test-environment
    sandboxTemplateName: bash
  - environmentSecretRef: other-environment
    sandboxTemplateName: other-bash
    sandboxTemplateNamespace: workloads
`), 0o600))
	writeEnvironmentCredentials(t, filepath.Join(root, "0"), "env_test", "test-key")
	writeEnvironmentCredentials(t, filepath.Join(root, "1"), "env_other", "other-key")

	environments, err := loadEnvironments(config, root, "release-namespace")

	require.NoError(t, err)
	require.Len(t, environments, 2)
	assert.Equal(t, "env_test", environments[0].ID)
	assert.Equal(t, "test-key", environments[0].Key)
	assert.Equal(t, "bash", environments[0].Template.GetName())
	assert.Equal(t, "release-namespace", environments[0].Template.GetNamespace())
	assert.Equal(t, "env_other", environments[1].ID)
	assert.Equal(t, "other-key", environments[1].Key)
	assert.Equal(t, "other-bash", environments[1].Template.GetName())
	assert.Equal(t, "workloads", environments[1].Template.GetNamespace())
}

func TestLoadEnvironmentRequiresSandboxTemplate(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config.yaml")
	require.NoError(t, os.WriteFile(config, []byte(`environments:
  - environmentSecretRef: test-environment
`), 0o600))
	writeEnvironmentCredentials(t, filepath.Join(root, "0"), "env_test", "test-key")

	_, err := loadEnvironments(config, root, "release-namespace")

	require.EqualError(t, err, "sandbox template name and namespace are required in environments[0]")
}

func TestWorkerRejectsDuplicateEnvironmentIDs(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config.yaml")
	require.NoError(t, os.WriteFile(config, []byte(`environments:
  - environmentSecretRef: first-secret
    sandboxTemplateName: bash
  - environmentSecretRef: second-secret
    sandboxTemplateName: bash
`), 0o600))
	writeEnvironmentCredentials(t, filepath.Join(root, "0"), "env_test", "first-key")
	writeEnvironmentCredentials(t, filepath.Join(root, "1"), "env_test\n", "second-key")
	environments, err := loadEnvironments(config, root, "release-namespace")
	require.NoError(t, err)

	_, err = anthropicselfhostedsandboxworker.New(nil, anthropic.Client{}, nil, environments, make([]byte, 32), time.Second, noop.Meter{})

	require.EqualError(t, err, `duplicate Anthropic environment ID "env_test"`)
}

func writeEnvironmentCredentials(t *testing.T, dir, id, key string) {
	t.Helper()
	require.NoError(t, os.Mkdir(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "environment-id"), []byte(id), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "environment-key"), []byte(key), 0o600))
}
