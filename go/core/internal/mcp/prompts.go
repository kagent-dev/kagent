package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const sandboxInstructions = `Use standalone Sandboxes for commands and files; their lifetime is independent of Sessions.
Discover a prepared SandboxTemplate with the programs needed for the task. Sandboxes currently have no allowed network egress; upload inputs and use dependencies already in the image. Work in /data/workspace.
Create with a unique request_id retained for retries, or reuse a sandbox belonging to the task. Check state, operation, and expires_at before work. Activity does not extend expiration.
Start a process once, retain its process_id, and use get_sandbox_process to check terminal status and exit_code. A lost start response may mean the process is running: never blindly retry a start.
Read output with read_sandbox_outputs, decode base64, and continue with both returned byte offsets. Empty output does not mean completion. MCP file transfers and output reads are limited to 1 MiB; file writes replace the destination.
Retrieve artifacts before deleting a scratch sandbox. Suspend may interrupt processes and transfers; files survive resume but process handles are not durable.
Lifecycle failures can leave work pending. Retry the same mutation with bounded backoff; creation retries require the same request_id and identical input. Get and List only observe state and never advance pending work.`

func registerSandboxPrompts(server *mcp.Server) {
	for _, definition := range []struct {
		prompt   *mcp.Prompt
		guidance string
	}{
		{
			prompt: &mcp.Prompt{
				Name: "sandbox-task", Description: "Complete a task in a temporary sandbox, verify results, and retrieve artifacts",
				Arguments: []*mcp.PromptArgument{
					{Name: "task", Description: "Work to perform and the desired deliverables", Required: true},
					{Name: "namespace", Description: "Optional namespace for template discovery"},
					{Name: "template", Description: "Optional SandboxTemplate name; requires namespace"},
				},
			},
			guidance: "Complete the supplied task using the available sandbox tools. Select a suitable environment, stage inputs, execute, verify the exit status and result, then retrieve deliverables. Delete scratch sandboxes created for this task after collection, unless retention was requested. Report retained IDs, expiration, and artifact locations. If required tools or templates are unavailable, explain the missing capability.\n\n" + sandboxInstructions,
		},
		{
			prompt: &mcp.Prompt{
				Name: "sandbox-recovery", Description: "Inspect an interrupted sandbox task and determine how to continue without duplicating work",
				Arguments: []*mcp.PromptArgument{
					{Name: "sandbox_id", Description: "UUID of the sandbox to inspect", Required: true},
				},
			},
			guidance: "Inspect this sandbox with get_sandbox first. Check state, operation, failure, and expiration. Use process IDs and creation inputs already recorded for the task; do not invent missing IDs. Inspect known result files for partial writes. Explain what finished, what remains uncertain, and the next recovery action. Resume or retry lifecycle work only as needed for the existing task; do not start duplicate commands or delete recoverable artifacts as a diagnostic step.\n\n" + sandboxInstructions,
		},
	} {
		server.AddPrompt(definition.prompt, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return sandboxPrompt(definition.prompt, definition.guidance, req.Params.Arguments)
		})
	}
}

func sandboxPrompt(prompt *mcp.Prompt, guidance string, arguments map[string]string) (*mcp.GetPromptResult, error) {
	allowed := make(map[string]bool, len(prompt.Arguments))
	for _, argument := range prompt.Arguments {
		allowed[argument.Name] = true
		if argument.Required && strings.TrimSpace(arguments[argument.Name]) == "" {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("%s is required", argument.Name)}
		}
	}
	for name := range arguments {
		if !allowed[name] {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("unknown prompt argument %q", name)}
		}
	}
	if arguments["template"] != "" && strings.TrimSpace(arguments["namespace"]) == "" {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "namespace is required when template is supplied"}
	}
	if prompt.Name == "sandbox-recovery" {
		if err := uuid.Validate(arguments["sandbox_id"]); err != nil {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "sandbox_id must be a UUID"}
		}
	}
	parameters, err := json.Marshal(arguments)
	if err != nil {
		return nil, err
	}
	return &mcp.GetPromptResult{
		Description: prompt.Description,
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: guidance}},
			{Role: "user", Content: &mcp.TextContent{Text: "Task parameters:\n" + string(parameters)}},
		},
	}, nil
}
