package utils_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNewKubeClientWrapper(t *testing.T) {
	t.Run("should create new wrapper with valid client", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		wrapper := utils.NewKubeClientWrapper(fakeClient)

		assert.NotNil(t, wrapper)
		assert.Implements(t, (*utils.KubeClientWrapper)(nil), wrapper)
	})
}

func TestAddInMemory(t *testing.T) {
	ctx := context.Background()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	wrapper := utils.NewKubeClientWrapper(fakeClient)

	t.Run("should add configmap to memory", func(t *testing.T) {
		configMap := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-config",
				Namespace: "test-namespace",
			},
			Data: map[string]string{
				"key": "value",
			},
		}

		err := wrapper.AddInMemory(configMap)
		require.NoError(t, err)

		// Try to get the object from memory
		retrievedConfig := &v1.ConfigMap{}
		err = wrapper.Get(ctx, types.NamespacedName{
			Name:      "test-config",
			Namespace: "test-namespace",
		}, retrievedConfig)

		require.NoError(t, err)
		assert.Equal(t, "test-config", retrievedConfig.Name)
		assert.Equal(t, "test-namespace", retrievedConfig.Namespace)
		assert.Equal(t, "value", retrievedConfig.Data["key"])
	})

	t.Run("should add secret to memory", func(t *testing.T) {
		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-namespace",
			},
			Data: map[string][]byte{
				"password": []byte("secret-value"),
			},
		}

		err := wrapper.AddInMemory(secret)
		require.NoError(t, err)

		// Try to get the object from memory
		retrievedSecret := &v1.Secret{}
		err = wrapper.Get(ctx, types.NamespacedName{
			Name:      "test-secret",
			Namespace: "test-namespace",
		}, retrievedSecret)

		require.NoError(t, err)
		assert.Equal(t, "test-secret", retrievedSecret.Name)
		assert.Equal(t, "test-namespace", retrievedSecret.Namespace)
		assert.Equal(t, []byte("secret-value"), retrievedSecret.Data["password"])
	})

	t.Run("should overwrite existing object in memory", func(t *testing.T) {
		configMap1 := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "overwrite-test",
				Namespace: "test-namespace",
			},
			Data: map[string]string{
				"key": "original-value",
			},
		}

		configMap2 := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "overwrite-test",
				Namespace: "test-namespace",
			},
			Data: map[string]string{
				"key": "updated-value",
			},
		}

		// Add first object
		err := wrapper.AddInMemory(configMap1)
		require.NoError(t, err)

		// Add second object with same name/namespace
		err = wrapper.AddInMemory(configMap2)
		require.NoError(t, err)

		// Retrieve and verify it's the updated object
		retrieved := &v1.ConfigMap{}
		err = wrapper.Get(ctx, types.NamespacedName{
			Name:      "overwrite-test",
			Namespace: "test-namespace",
		}, retrieved)

		require.NoError(t, err)
		assert.Equal(t, "updated-value", retrieved.Data["key"])
	})
}

func TestGet(t *testing.T) {
	ctx := context.Background()

	t.Run("should get object from memory cache", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		wrapper := utils.NewKubeClientWrapper(fakeClient)

		// Add object to memory
		configMap := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cached-config",
				Namespace: "test-namespace",
			},
			Data: map[string]string{
				"source": "memory",
			},
		}
		err := wrapper.AddInMemory(configMap)
		require.NoError(t, err)

		// Get object (should come from memory)
		retrieved := &v1.ConfigMap{}
		err = wrapper.Get(ctx, types.NamespacedName{
			Name:      "cached-config",
			Namespace: "test-namespace",
		}, retrieved)

		require.NoError(t, err)
		assert.Equal(t, "memory", retrieved.Data["source"])
	})

	t.Run("should get object from underlying client when not in cache", func(t *testing.T) {
		// Create object in fake client
		configMap := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "k8s-config",
				Namespace: "test-namespace",
			},
			Data: map[string]string{
				"source": "kubernetes",
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithObjects(configMap).
			Build()

		wrapper := utils.NewKubeClientWrapper(fakeClient)

		// Get object (should come from underlying client)
		retrieved := &v1.ConfigMap{}
		err := wrapper.Get(ctx, types.NamespacedName{
			Name:      "k8s-config",
			Namespace: "test-namespace",
		}, retrieved)

		require.NoError(t, err)
		assert.Equal(t, "kubernetes", retrieved.Data["source"])
	})

	t.Run("should prioritize memory cache over underlying client", func(t *testing.T) {
		// Create object in fake client
		k8sConfigMap := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "priority-test",
				Namespace: "test-namespace",
			},
			Data: map[string]string{
				"source": "kubernetes",
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithObjects(k8sConfigMap).
			Build()

		wrapper := utils.NewKubeClientWrapper(fakeClient)

		// Add different object with same key to memory
		memoryConfigMap := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "priority-test",
				Namespace: "test-namespace",
			},
			Data: map[string]string{
				"source": "memory",
			},
		}
		err := wrapper.AddInMemory(memoryConfigMap)
		require.NoError(t, err)

		// Get object - should come from memory, not kubernetes
		retrieved := &v1.ConfigMap{}
		err = wrapper.Get(ctx, types.NamespacedName{
			Name:      "priority-test",
			Namespace: "test-namespace",
		}, retrieved)

		require.NoError(t, err)
		assert.Equal(t, "memory", retrieved.Data["source"])
	})

	t.Run("should return error when object not found anywhere", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		wrapper := utils.NewKubeClientWrapper(fakeClient)

		retrieved := &v1.ConfigMap{}
		err := wrapper.Get(ctx, types.NamespacedName{
			Name:      "nonexistent",
			Namespace: "test-namespace",
		}, retrieved)

		assert.Error(t, err)
	})
}

func TestConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	wrapper := utils.NewKubeClientWrapper(fakeClient)

	t.Run("should handle concurrent AddInMemory and Get operations", func(t *testing.T) {
		var wg sync.WaitGroup
		numRoutines := 10

		// Start multiple goroutines adding objects
		for i := 0; i < numRoutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				configMap := &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("concurrent-config-%d", id),
						Namespace: "test-namespace",
					},
					Data: map[string]string{
						"id": fmt.Sprintf("%d", id),
					},
				}

				err := wrapper.AddInMemory(configMap)
				assert.NoError(t, err)
			}(i)
		}

		// Start multiple goroutines reading objects
		for i := 0; i < numRoutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				retrieved := &v1.ConfigMap{}
				err := wrapper.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("concurrent-config-%d", id),
					Namespace: "test-namespace",
				}, retrieved)

				// May error if the object hasn't been added yet, which is okay
				if err == nil {
					assert.Equal(t, fmt.Sprintf("%d", id), retrieved.Data["id"])
				}
			}(i)
		}

		wg.Wait()

		// Verify all objects are accessible after concurrent operations
		for i := 0; i < numRoutines; i++ {
			retrieved := &v1.ConfigMap{}
			err := wrapper.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("concurrent-config-%d", i),
				Namespace: "test-namespace",
			}, retrieved)

			require.NoError(t, err)
			assert.Equal(t, fmt.Sprintf("%d", i), retrieved.Data["id"])
		}
	})
}

func TestDifferentObjectTypes(t *testing.T) {
	ctx := context.Background()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	wrapper := utils.NewKubeClientWrapper(fakeClient)

	t.Run("should handle different object types independently", func(t *testing.T) {
		// Add ConfigMap
		configMap := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "same-name",
				Namespace: "test-namespace",
			},
			Data: map[string]string{
				"type": "configmap",
			},
		}
		err := wrapper.AddInMemory(configMap)
		require.NoError(t, err)

		// Add Secret with same name and namespace
		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "same-name",
				Namespace: "test-namespace",
			},
			Data: map[string][]byte{
				"type": []byte("secret"),
			},
		}
		err = wrapper.AddInMemory(secret)
		require.NoError(t, err)

		// Retrieve ConfigMap
		retrievedConfig := &v1.ConfigMap{}
		err = wrapper.Get(ctx, types.NamespacedName{
			Name:      "same-name",
			Namespace: "test-namespace",
		}, retrievedConfig)
		require.NoError(t, err)
		assert.Equal(t, "configmap", retrievedConfig.Data["type"])

		// Retrieve Secret
		retrievedSecret := &v1.Secret{}
		err = wrapper.Get(ctx, types.NamespacedName{
			Name:      "same-name",
			Namespace: "test-namespace",
		}, retrievedSecret)
		require.NoError(t, err)
		assert.Equal(t, []byte("secret"), retrievedSecret.Data["type"])
	})
}

// mockObject is a simple test object that implements client.Object
type mockObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Data              string `json:"data,omitempty"`
}

func (m *mockObject) DeepCopyObject() runtime.Object {
	return &mockObject{
		TypeMeta:   m.TypeMeta,
		ObjectMeta: *m.ObjectMeta.DeepCopy(),
		Data:       m.Data,
	}
}

func TestInvalidScheme(t *testing.T) {
	t.Run("should handle objects not in scheme gracefully", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		wrapper := utils.NewKubeClientWrapper(fakeClient)

		// Create an object that's not registered in the scheme
		mockObj := &mockObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mock-object",
				Namespace: "test-namespace",
			},
			Data: "test-data",
		}

		err := wrapper.AddInMemory(mockObj)
		// This should fail because mockObject is not in the scheme
		assert.Error(t, err)
	})
}
