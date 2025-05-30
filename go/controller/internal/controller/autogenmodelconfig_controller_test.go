/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentv1alpha1 "github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
)

func TestAutogenModelConfigController(t *testing.T) {
	t.Run("When reconciling a resource", func(t *testing.T) {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		autogenmodelconfig := &agentv1alpha1.ModelConfig{}

		// Setup - creating the custom resource for the Kind AutogenModelConfig
		err := k8sClient.Get(ctx, typeNamespacedName, autogenmodelconfig)
		if err != nil && errors.IsNotFound(err) {
			resource := &agentv1alpha1.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				// TODO(user): Specify other spec details if needed.
			}
			require.NoError(t, k8sClient.Create(ctx, resource))
		}

		// Cleanup function
		t.Cleanup(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &agentv1alpha1.ModelConfig{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			require.NoError(t, err)

			// Cleanup the specific resource instance AutogenModelConfig
			require.NoError(t, k8sClient.Delete(ctx, resource))
		})

		t.Run("should successfully reconcile the resource", func(t *testing.T) {
			// Reconciling the created resource
			controllerReconciler := &AutogenModelConfigReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			assert.NoError(t, err)
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
}
