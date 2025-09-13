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

package v1alpha2

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/internal/utils"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ValueSourceType string

const (
	ConfigMapValueSource ValueSourceType = "ConfigMap"
	SecretValueSource    ValueSourceType = "Secret"
)

// ValueSource defines a source for configuration values from a Secret or ConfigMap
type ValueSource struct {
	// +kubebuilder:validation:Enum=ConfigMap;Secret
	Type ValueSourceType `json:"type"`
	// The name of the ConfigMap or Secret.
	Name string `json:"name"`
	// The key of the ConfigMap or Secret.
	Key string `json:"key"`
}

func (s *ValueSource) Resolve(ctx context.Context, client client.Client, namespace string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("ValueSource cannot be nil")
	}

	switch s.Type {
	case ConfigMapValueSource:
		return utils.GetConfigMapValue(ctx, client, types.NamespacedName{Namespace: namespace, Name: s.Name}, s.Key)
	case SecretValueSource:
		return utils.GetSecretValue(ctx, client, types.NamespacedName{Namespace: namespace, Name: s.Name}, s.Key)
	default:
		return "", fmt.Errorf("unknown value source type: %s", s.Type)
	}
}

// ValueRef represents a configuration value
// +kubebuilder:validation:XValidation:rule="(has(self.value) && !has(self.valueFrom)) || (!has(self.value) && has(self.valueFrom))",message="Exactly one of value or valueFrom must be specified"
type ValueRef struct {
	Name string `json:"name"`
	// +optional
	Value string `json:"value,omitempty"`
	// +optional
	ValueFrom *ValueSource `json:"valueFrom,omitempty"`
}

func (r *ValueRef) Resolve(ctx context.Context, client client.Client, namespace string) (string, string, error) {
	if r == nil {
		return "", "", fmt.Errorf("ValueRef cannot be nil")
	}

	switch {
	case r.Value != "":
		return r.Name, r.Value, nil
	case r.ValueFrom != nil:
		value, err := r.ValueFrom.Resolve(ctx, client, namespace)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve value for ref %s: %v", r.Name, err)
		}

		return r.Name, value, nil
	default:
		return r.Name, "", nil
	}
}
