package translator

import "fmt"

// ContextWarnings reports each AgentTemplate in the tree whose spec.context
// the compilation ignores, with the reason. The root is included when
// includeRoot is set, for a harness that does not apply the setting at all.
// Shared children are always included: context compaction belongs to the
// runner that drives the root agent, so a child's setting is never read.
func ContextWarnings(root *AgentInput, includeRoot bool, reason string) []string {
	var warnings []string
	if includeRoot && root.Template != nil && root.Template.Spec.Context != nil {
		warnings = append(warnings, fmt.Sprintf("AgentTemplate %q spec.context is ignored: %s", root.Template.Name, reason))
	}
	for _, binding := range root.Shared {
		if binding.Agent == nil {
			continue
		}
		warnings = append(warnings, ContextWarnings(binding.Agent, true, reason)...)
	}
	return warnings
}
