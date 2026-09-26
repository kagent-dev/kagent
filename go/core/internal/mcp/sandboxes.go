package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"buf.build/go/protovalidate"
	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/kagent-dev/kagent/go/core/internal/service/sandbox"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/durationpb"
)

const sandboxToolBytes = 1 << 20

type sandboxInput struct {
	SandboxID string `json:"sandbox_id" jsonschema:"Sandbox UUID"`
}

type sandboxSummary struct {
	ID        string                         `json:"id"`
	Template  *apiv1alpha1.ResourceReference `json:"sandbox_template"`
	Name      string                         `json:"name,omitempty"`
	State     string                         `json:"state"`
	Operation string                         `json:"operation"`
	ExpiresAt string                         `json:"expires_at"`
	Failure   *apiv1alpha1.Failure           `json:"failure,omitempty"`
}

type sandboxCreateInput struct {
	Namespace  string `json:"namespace"`
	Template   string `json:"template" jsonschema:"SandboxTemplate name"`
	RequestID  string `json:"request_id" jsonschema:"Stable idempotency key; reuse for retries with identical inputs"`
	Name       string `json:"name,omitempty"`
	TTLSeconds int64  `json:"ttl_seconds,omitempty" jsonschema:"Lifetime in seconds; omission uses operator policy"`
}

type sandboxListInput struct {
	PageSize  int32  `json:"page_size,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

type sandboxListOutput struct {
	Sandboxes     []sandboxSummary `json:"sandboxes"`
	NextPageToken string           `json:"next_page_token,omitempty"`
}

type sandboxProcessInput struct {
	SandboxID string `json:"sandbox_id"`
	ProcessID string `json:"process_id"`
}

type sandboxStartInput struct {
	SandboxID string            `json:"sandbox_id"`
	Command   []string          `json:"command" jsonschema:"Executable and arguments; use sh -c explicitly for shell syntax"`
	CWD       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

type sandboxStartOutput struct {
	ProcessID string `json:"process_id"`
}

type sandboxProcessOutput struct {
	ProcessID string `json:"process_id"`
	Status    string `json:"status"`
	ExitCode  int32  `json:"exit_code"`
}

type sandboxOutputsInput struct {
	SandboxID    string `json:"sandbox_id"`
	ProcessID    string `json:"process_id"`
	StdoutOffset int64  `json:"stdout_offset,omitempty"`
	StderrOffset int64  `json:"stderr_offset,omitempty"`
}

type sandboxOutputsOutput struct {
	StdoutBase64 string `json:"stdout_base64"`
	StderrBase64 string `json:"stderr_base64"`
	StdoutOffset int64  `json:"stdout_offset"`
	StderrOffset int64  `json:"stderr_offset"`
	Truncated    bool   `json:"truncated"`
}

type sandboxReadInput struct {
	SandboxID string `json:"sandbox_id"`
	Path      string `json:"path"`
}

type sandboxReadOutput struct {
	DataBase64 string `json:"data_base64"`
}

type sandboxWriteInput struct {
	SandboxID  string `json:"sandbox_id"`
	Path       string `json:"path"`
	DataBase64 string `json:"data_base64" jsonschema:"Base64 file contents, up to 1 MiB decoded"`
	Mode       uint32 `json:"mode,omitempty" jsonschema:"Unix permission bits in decimal; omission uses guest defaults"`
}

type sandboxWriteOutput struct {
	BytesWritten int64 `json:"bytes_written"`
}

func summarizeSandbox(value *apiv1alpha1.Sandbox) sandboxSummary {
	return sandboxSummary{ID: value.Id, Template: value.SandboxTemplate, Name: value.Name,
		State: value.State.String(), Operation: value.Operation.String(), ExpiresAt: value.ExpiresAt.AsTime().Format(time.RFC3339Nano), Failure: value.Failure}
}

// The MCP SDK supplies schemas and JSON content. Service errors belong in tool
// results so an agent can correct its request without losing the MCP session.
func addSandboxTool[In, Out any](server *mcp.Server, name, description string, call func(context.Context, In) (Out, error)) {
	mcp.AddTool(server, &mcp.Tool{Name: name, Description: description}, func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		output, err := call(ctx, input)
		if err != nil {
			return toolError(errors.New(serviceerrors.MessageOf(err))), output, nil
		}
		return nil, output, nil
	})
}

func registerSandboxTools(server *mcp.Server, service *sandbox.Service, templates *kubecrud.Service[*v1alpha3.SandboxTemplate, *v1alpha3.SandboxTemplateList]) {
	if templates != nil {
		type input struct {
			Namespace string `json:"namespace,omitempty"`
		}
		type output struct {
			Templates []apiv1alpha1.ResourceReference `json:"templates"`
		}
		addSandboxTool(server, "list_sandbox_templates", "List SandboxTemplates visible to you", func(ctx context.Context, in input) (output, error) {
			values, err := templates.List(ctx, in.Namespace)
			result := output{Templates: make([]apiv1alpha1.ResourceReference, 0, len(values))}
			for _, value := range values {
				result.Templates = append(result.Templates, apiv1alpha1.ResourceReference{Namespace: value.Namespace, Name: value.Name})
			}
			return result, err
		})
	}
	if service == nil {
		return
	}
	addSandboxTool(server, "create_sandbox", "Create a temporary workspace from a prepared SandboxTemplate", func(ctx context.Context, in sandboxCreateInput) (sandboxSummary, error) {
		request := &apiv1alpha1.CreateSandboxRequest{SandboxTemplate: &apiv1alpha1.ResourceReference{Namespace: in.Namespace, Name: in.Template}, RequestId: in.RequestID, Name: in.Name}
		if in.TTLSeconds != 0 {
			request.Ttl = &durationpb.Duration{Seconds: in.TTLSeconds}
		}
		if err := protovalidate.Validate(request); err != nil {
			return sandboxSummary{}, err
		}
		value, err := service.Create(ctx, request)
		if err != nil {
			return sandboxSummary{}, err
		}
		return summarizeSandbox(value), nil
	})
	addSandboxTool(server, "list_sandboxes", "List your temporary workspaces", func(ctx context.Context, in sandboxListInput) (sandboxListOutput, error) {
		request := &apiv1alpha1.ListSandboxesRequest{Page: &apiv1alpha1.PageRequest{Limit: in.PageSize, PageToken: in.PageToken}}
		if err := protovalidate.Validate(request); err != nil {
			return sandboxListOutput{}, err
		}
		response, err := service.List(ctx, request)
		if err != nil {
			return sandboxListOutput{}, err
		}
		result := sandboxListOutput{Sandboxes: make([]sandboxSummary, 0, len(response.Sandboxes)), NextPageToken: response.GetPage().GetNextPageToken()}
		for _, value := range response.Sandboxes {
			result.Sandboxes = append(result.Sandboxes, summarizeSandbox(value))
		}
		return result, nil
	})
	for _, action := range []struct {
		name, description string
		call              func(context.Context, string) (*apiv1alpha1.Sandbox, error)
	}{
		{"get_sandbox", "Inspect a workspace and its expiration", service.Get},
		{"suspend_sandbox", "Suspend a workspace; running commands and transfers may be interrupted", service.Suspend},
		{"resume_sandbox", "Resume a workspace; previous guest process handles are no longer valid", service.Resume},
		{"delete_sandbox", "Delete a workspace and its files", service.Delete},
	} {
		addSandboxTool(server, action.name, action.description, func(ctx context.Context, in sandboxInput) (sandboxSummary, error) {
			if err := protovalidate.Validate(&apiv1alpha1.GetSandboxRequest{SandboxId: in.SandboxID}); err != nil {
				return sandboxSummary{}, err
			}
			value, err := action.call(ctx, in.SandboxID)
			if err != nil {
				return sandboxSummary{}, err
			}
			return summarizeSandbox(value), nil
		})
	}
	addSandboxTool(server, "start_sandbox_process", "Start a new process; retrying after an uncertain result may execute the command again", func(ctx context.Context, in sandboxStartInput) (sandboxStartOutput, error) {
		request := &guestpb.StartProcessRequest{Command: in.Command, Cwd: in.CWD, Env: in.Env}
		result, err := service.StartProcess(ctx, in.SandboxID, request)
		return sandboxStartOutput{ProcessID: result.GetProcessId()}, err
	})
	addSandboxTool(server, "get_sandbox_process", "Inspect a process and its exit status", func(ctx context.Context, in sandboxProcessInput) (sandboxProcessOutput, error) {
		result, err := service.GetProcess(ctx, in.SandboxID, &guestpb.GetProcessRequest{ProcessId: in.ProcessID})
		return sandboxProcessOutput{ProcessID: result.GetProcessId(), Status: result.GetStatus().String(), ExitCode: result.GetExitCode()}, err
	})
	addSandboxTool(server, "kill_sandbox_process", "Terminate a sandbox process", func(ctx context.Context, in sandboxProcessInput) (sandboxProcessOutput, error) {
		result, err := service.KillProcess(ctx, in.SandboxID, &guestpb.KillProcessRequest{ProcessId: in.ProcessID})
		return sandboxProcessOutput{ProcessID: in.ProcessID, ExitCode: result.GetExitCode()}, err
	})
	addSandboxTool(server, "read_sandbox_outputs", "Read currently available output, up to 1 MiB; pass returned byte offsets to continue", func(ctx context.Context, in sandboxOutputsInput) (sandboxOutputsOutput, error) {
		request := &guestpb.StreamProcessOutputsRequest{ProcessId: in.ProcessID, StdoutOffset: in.StdoutOffset, StderrOffset: in.StderrOffset}
		result := sandboxOutputsOutput{StdoutOffset: in.StdoutOffset, StderrOffset: in.StderrOffset}
		var stdout, stderr []byte
		limit := errors.New("output limit reached")
		err := service.StreamProcessOutputs(ctx, in.SandboxID, request, func(chunk *guestpb.OutputChunk) error {
			data := chunk.Data
			if remaining := sandboxToolBytes - len(stdout) - len(stderr); len(data) > remaining {
				data = data[:remaining]
				result.Truncated = true
			}
			switch chunk.Source {
			case guestpb.OutputSource_OUTPUT_SOURCE_STDOUT:
				stdout = append(stdout, data...)
				result.StdoutOffset += int64(len(data))
			case guestpb.OutputSource_OUTPUT_SOURCE_STDERR:
				stderr = append(stderr, data...)
				result.StderrOffset += int64(len(data))
			default:
				return fmt.Errorf("unknown guest output source %s", chunk.Source)
			}
			if result.Truncated {
				return limit
			}
			return nil
		})
		if errors.Is(err, limit) {
			err = nil
		}
		result.StdoutBase64, result.StderrBase64 = base64.StdEncoding.EncodeToString(stdout), base64.StdEncoding.EncodeToString(stderr)
		return result, err
	})
	addSandboxTool(server, "read_sandbox_file", "Read a file up to 1 MiB as base64", func(ctx context.Context, in sandboxReadInput) (sandboxReadOutput, error) {
		var data []byte
		err := service.ReadFile(ctx, in.SandboxID, &guestpb.ReadFileRequest{Path: in.Path}, func(chunk *guestpb.FileChunk) error {
			if len(data)+len(chunk.Data) > sandboxToolBytes {
				return fmt.Errorf("file exceeds MCP 1 MiB limit; use the streaming API")
			}
			data = append(data, chunk.Data...)
			return nil
		})
		return sandboxReadOutput{DataBase64: base64.StdEncoding.EncodeToString(data)}, err
	})
	addSandboxTool(server, "write_sandbox_file", "Write a file from base64 data, up to 1 MiB", func(ctx context.Context, in sandboxWriteInput) (sandboxWriteOutput, error) {
		header := &guestpb.WriteFileRequest{Path: in.Path, Mode: in.Mode}
		if base64.StdEncoding.DecodedLen(len(in.DataBase64)) > sandboxToolBytes+2 {
			return sandboxWriteOutput{}, fmt.Errorf("file exceeds MCP 1 MiB limit")
		}
		data, err := base64.StdEncoding.DecodeString(in.DataBase64)
		if err != nil {
			return sandboxWriteOutput{}, fmt.Errorf("invalid base64: %w", err)
		}
		if len(data) > sandboxToolBytes {
			return sandboxWriteOutput{}, fmt.Errorf("file exceeds MCP 1 MiB limit")
		}
		result, err := service.WriteFile(ctx, in.SandboxID, func() (*guestpb.WriteFileRequest, error) {
			if header != nil {
				first := header
				header = nil
				return first, nil
			}
			if len(data) == 0 {
				return nil, io.EOF
			}
			n := min(len(data), 256<<10)
			chunk := &guestpb.WriteFileRequest{Chunk: data[:n]}
			data = data[n:]
			return chunk, nil
		})
		return sandboxWriteOutput{BytesWritten: result.GetBytesWritten()}, err
	})
}
