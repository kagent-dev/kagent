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

// Package toolcatalog contains semantic operations shared by MCP catalog
// reconcilers.
package toolcatalog

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	toolservice "github.com/kagent-dev/kagent/go/core/internal/service/tool"
)

// NormalizeTools validates and deterministically orders discovered MCP tools.
func NormalizeTools(tools []toolservice.MCPAppTool) ([]*v1alpha3.MCPTool, error) {
	discovered := make([]*v1alpha3.MCPTool, 0, len(tools))
	seen := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" {
			return nil, fmt.Errorf("MCP discovery returned a tool with an empty name")
		}
		if _, exists := seen[tool.Name]; exists {
			return nil, fmt.Errorf("MCP discovery returned duplicate tool %q", tool.Name)
		}
		seen[tool.Name] = struct{}{}
		discovered = append(discovered, &v1alpha3.MCPTool{Name: tool.Name, Description: tool.Description})
	}
	slices.SortFunc(discovered, func(a, b *v1alpha3.MCPTool) int {
		return strings.Compare(a.Name, b.Name)
	})
	return discovered, nil
}
