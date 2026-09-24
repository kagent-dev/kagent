/*
Copyright 2026.

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

package toolcatalog

import (
	"testing"

	toolservice "github.com/kagent-dev/kagent/go/core/internal/service/tool"
)

func TestNormalizeTools(t *testing.T) {
	tests := []struct {
		name  string
		tools []toolservice.MCPAppTool
		want  []string
	}{
		{name: "sorted", tools: []toolservice.MCPAppTool{{Name: "zeta"}, {Name: "alpha"}}, want: []string{"alpha", "zeta"}},
		{name: "empty name", tools: []toolservice.MCPAppTool{{Name: " "}}},
		{name: "duplicate name", tools: []toolservice.MCPAppTool{{Name: "lookup"}, {Name: "lookup"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeTools(test.tools)
			if test.want == nil {
				if err == nil {
					t.Fatal("NormalizeTools() error = nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeTools() error = %v", err)
			}
			if len(got) != len(test.want) {
				t.Fatalf("NormalizeTools() = %#v", got)
			}
			for index := range got {
				if got[index].Name != test.want[index] {
					t.Fatalf("NormalizeTools()[%d] = %q, want %q", index, got[index].Name, test.want[index])
				}
			}
		})
	}
}
