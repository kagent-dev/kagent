package declarative

import (
	"fmt"

	"github.com/kagent-dev/kagent/go/adk/pkg/tools"
)

// HarnessTools are the basic tools of the kagent harness that
// RegisterHarnessTools adds to the local tool library: read and write a file,
// list a directory, and run a shell command. Relative paths and the shell's
// working directory are the workspace; isolation comes from the sandbox the
// workflow runs in.
var HarnessTools = []string{"read_file", "write_file", "list_files", "bash"}

// HarnessConfig selects the workspace of the harness tools.
type HarnessConfig struct {
	// Workspace is the working directory; empty uses the actor workspace.
	Workspace string
	// DisableShell leaves out the bash tool.
	DisableShell bool
}

// RegisterHarnessTools registers the HarnessTools so that documents can use
// them as tool nodes (type: tool, tool: write_file) and in LLM tool lists.
// They are opt-in: a registry without them cannot touch the filesystem.
func (r *Registry) RegisterHarnessTools(cfg HarnessConfig) error {
	builtin, err := tools.NewBuiltinTools(tools.BuiltinToolsConfig{Workspace: cfg.Workspace, DisableBash: cfg.DisableShell})
	if err != nil {
		return fmt.Errorf("failed to create harness tools: %w", err)
	}
	wanted := map[string]bool{}
	for _, name := range HarnessTools {
		wanted[name] = true
	}
	for _, t := range builtin {
		if !wanted[t.Name()] {
			continue
		}
		if err := r.RegisterTool(t); err != nil {
			return err
		}
	}
	return nil
}
