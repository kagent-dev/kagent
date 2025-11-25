package reconciler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// TestComputeStatusSecretHash_Output verifies the output of the hash function
func TestComputeStatusSecretHash_Output(t *testing.T) {
	tests := []struct {
		name    string
		secrets []secretRef
		want    string
	}{
		{
			name:    "no secrets",
			secrets: []secretRef{},
			want:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", // i.e. the hash of an empty string
		},
		{
			name: "one secret, no keys",
			secrets: []secretRef{
				{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
					Secret: &corev1.Secret{
						Data: map[string][]byte{},
					},
				},
			},
			want: "68a268d3f02147004cfa8b609966ec4cba7733f8c652edb80be8071eb1b91574", // because the secret exists, it still hashes the namespacedName + empty data
		},
		{
			name: "one secret, single key",
			secrets: []secretRef{
				{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
					Secret: &corev1.Secret{
						Data: map[string][]byte{"key1": []byte("value1")},
					},
				},
			},
			want: "62dc22ecd609281a5939efd60fae775e6b75b641614c523c400db994a09902ff",
		},
		{
			name: "one secret, multiple keys",
			secrets: []secretRef{
				{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
					Secret: &corev1.Secret{
						Data: map[string][]byte{"key1": []byte("value1"), "key2": []byte("value2")},
					},
				},
			},
			want: "ba6798ec591d129f78322cdae569eaccdb2f5a8343c12026f0ed6f4e156cd52e",
		},
		{
			name: "multiple secrets",
			secrets: []secretRef{
				{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
					Secret: &corev1.Secret{
						Data: map[string][]byte{"key1": []byte("value1")},
					},
				},
				{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret2"},
					Secret: &corev1.Secret{
						Data: map[string][]byte{"key2": []byte("value2")},
					},
				},
			},
			want: "f174f0e21a4427a87a23e4f277946a27f686d023cbe42f3000df94a4df94f7b5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeStatusSecretHash(tt.secrets)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestComputeStatusSecretHash_Deterministic tests that the resultant hash is deterministic, specifically that ordering of keys and secrets does not matter
func TestComputeStatusSecretHash_Deterministic(t *testing.T) {
	tests := []struct {
		name          string
		secrets       [2][]secretRef
		expectedEqual bool
	}{
		{
			name: "key ordering should not matter",
			secrets: [2][]secretRef{
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1"), "key2": []byte("value2")},
						},
					},
				},
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key2": []byte("value2"), "key1": []byte("value1")},
						},
					},
				},
			},
			expectedEqual: true,
		},
		{
			name: "secret ordering should not matter",
			secrets: [2][]secretRef{
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1")},
						},
					},
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret2"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1")},
						},
					},
				},
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret2"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1")},
						},
					},
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1")},
						},
					},
				},
			},
			expectedEqual: true,
		},
		{
			name: "secret and key ordering should not matter",
			secrets: [2][]secretRef{
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1"), "key2": []byte("value2")},
						},
					},
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret2"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key2": []byte("value2"), "key1": []byte("value1")},
						},
					},
				},
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret2"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1"), "key2": []byte("value2")},
						},
					},
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key2": []byte("value2"), "key1": []byte("value1")},
						},
					},
				},
			},
			expectedEqual: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got1 := computeStatusSecretHash(tt.secrets[0])
			got2 := computeStatusSecretHash(tt.secrets[1])
			assert.Equal(t, tt.expectedEqual, got1 == got2)
		})
	}
}
