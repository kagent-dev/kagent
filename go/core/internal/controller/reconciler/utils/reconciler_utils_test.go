package utils

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestDistinctByType is the dedup that lets SetupOwnerIndexes accept the same
// type from several owners (e.g. a type owned by both the agent translator and
// a RemoteMCPServer plugin) without IndexField erroring on a duplicate field
// registration.
func TestDistinctByType(t *testing.T) {
	t.Run("removes duplicate types, keeps first-occurrence order", func(t *testing.T) {
		in := []client.Object{
			&corev1.ConfigMap{}, // translator
			&corev1.ConfigMap{}, // plugin (overlap)
			&corev1.Secret{},    // plugin-only
		}
		got := distinctByType(in)

		require.Len(t, got, 2, "duplicate ConfigMap must collapse to one entry")
		assert.IsType(t, &corev1.ConfigMap{}, got[0], "first occurrence order preserved")
		assert.IsType(t, &corev1.Secret{}, got[1])

		counts := map[reflect.Type]int{}
		for _, o := range got {
			counts[reflect.TypeOf(o)]++
		}
		assert.Equal(t, 1, counts[reflect.TypeFor[*corev1.ConfigMap]()])
		assert.Equal(t, 1, counts[reflect.TypeFor[*corev1.Secret]()])
	})

	t.Run("nil-typed entries are dropped", func(t *testing.T) {
		got := distinctByType([]client.Object{nil, &corev1.Secret{}, nil})
		require.Len(t, got, 1)
		assert.IsType(t, &corev1.Secret{}, got[0])
	})

	t.Run("empty input yields empty output", func(t *testing.T) {
		assert.Empty(t, distinctByType(nil))
	})
}
