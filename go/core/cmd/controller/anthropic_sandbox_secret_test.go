package main

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAnthropicSandboxEncryptionSecret(t *testing.T) {
	key := client.ObjectKey{Namespace: "workloads", Name: "tenant-anthropic-sandbox-encryption"}
	kube := fake.NewClientBuilder().Build()

	require.NoError(t, ensureAnthropicSandboxEncryptionSecret(t.Context(), kube, kube, key))

	created := &corev1.Secret{}
	require.NoError(t, kube.Get(t.Context(), key, created))
	decoded, err := base64.StdEncoding.DecodeString(string(created.Data["key"]))
	require.NoError(t, err)
	require.Len(t, decoded, 32)
	require.Empty(t, created.OwnerReferences, "the key must outlive the release")

	for _, scenario := range []struct {
		name   string
		reader client.Reader
	}{
		{"Given an existing key, when the controller restarts, assert that the key is preserved", kube},
		{"Given a stale read, when two controllers try to create the key, assert that the winning key is preserved", fake.NewClientBuilder().Build()},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.NoError(t, ensureAnthropicSandboxEncryptionSecret(t.Context(), scenario.reader, kube, key))

			persisted := &corev1.Secret{}
			require.NoError(t, kube.Get(t.Context(), key, persisted))
			require.Equal(t, created.Data["key"], persisted.Data["key"])
		})
	}
}
