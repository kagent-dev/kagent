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

package predicates

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestDiscoveryDisabledPredicate(t *testing.T) {
	predicate := DiscoveryDisabledPredicate{}

	tests := []struct {
		name     string
		labels   map[string]string
		expected bool
	}{
		{
			name:     "no label - should process",
			labels:   nil,
			expected: true,
		},
		{
			name:     "discovery label set to disabled - should not process",
			labels:   map[string]string{"kagent.dev/discovery": "disabled"},
			expected: false,
		},
		{
			name:     "discovery label set to enabled - should process",
			labels:   map[string]string{"kagent.dev/discovery": "enabled"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testObj := &unstructured.Unstructured{}
			testObj.SetLabels(tt.labels)

			// Test all event types with the same test cases
			createEvent := event.CreateEvent{Object: testObj}
			updateEvent := event.UpdateEvent{ObjectNew: testObj}
			deleteEvent := event.DeleteEvent{Object: testObj}
			genericEvent := event.GenericEvent{Object: testObj}

			assert.Equal(t, tt.expected, predicate.Create(createEvent), "Create event should match expected result")
			assert.Equal(t, tt.expected, predicate.Update(updateEvent), "Update event should match expected result")
			assert.Equal(t, tt.expected, predicate.Delete(deleteEvent), "Delete event should match expected result")
			assert.Equal(t, tt.expected, predicate.Generic(genericEvent), "Generic event should match expected result")
		})
	}
}
