package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"time"

	"buf.build/go/protovalidate"
	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/kagent-dev/kagent/go/core/internal/service/sandbox"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"google.golang.org/protobuf/types/known/durationpb"
)

const sandboxToolBytes = 1 << 20

const sandboxToolTextBytes = 32 << 10

const (
	// Claude Code downscales larger images before the model sees them.
	sandboxImageMaxPixels = 2000
	// Claude Code re-encodes larger images, so smaller ones reach the model
	// untouched.
	sandboxImageResultBytes = 500_000
	sandboxImageBytes       = 16 << 20
	// Decoding allocates per pixel, so larger images are refused unread.
	sandboxImageMaxArea = 50_000_000
)

// Codex otherwise downscales images to its default detail.
var sandboxImageMeta = mcp.Meta{"codex/imageDetail": "original"}

const (
	sandboxFileLines     = 2000
	sandboxFileLineBytes = 2000
)

var errReadDone = errors.New("read done")

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
	Offset    int    `json:"offset,omitempty" jsonschema:"First line of a text file to read, from 1"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Lines of a text file to read; defaults to 2000"`
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

func textContent(format string, args ...any) []mcp.Content {
	return []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}}
}

// fileReader classifies a file from its first bytes, then pages text by line,
// keeps an image whole, and reads no further into any other file.
type fileReader struct {
	sniffed, text, tooLarge bool
	mediaType               string
	data                    []byte
	page                    linePage
}

func (r *fileReader) write(chunk []byte) error {
	if r.text {
		return r.page.write(chunk)
	}
	r.data = append(r.data, chunk...)
	if !r.sniffed && len(r.data) >= 512 {
		if err := r.sniff(); err != nil || r.text {
			return err
		}
	}
	if r.sniffed && !strings.HasPrefix(r.mediaType, "image/") {
		return errReadDone
	}
	if len(r.data) > sandboxImageBytes {
		r.tooLarge = true
		return errReadDone
	}
	return nil
}

// sniff decides on the bytes http.DetectContentType reads.
func (r *fileReader) sniff() error {
	r.sniffed = true
	r.mediaType = http.DetectContentType(r.data)
	r.text = strings.HasPrefix(r.mediaType, "text/")
	if !r.text {
		return nil
	}
	data := r.data
	r.data = nil
	return r.page.write(data)
}

// content decodes images with the registered PNG, JPEG, GIF and WebP decoders,
// all formats that models accept.
func (r *fileReader) content() []mcp.Content {
	if !r.sniffed {
		_ = r.sniff()
	}
	if r.text {
		return textContent("%s", r.page.finish())
	}
	if r.tooLarge {
		return textContent("%s image over 16 MiB, too large to read", r.mediaType)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(r.data))
	if err != nil {
		return textContent("binary file, %s", r.mediaType)
	}
	if config.Width*config.Height > sandboxImageMaxArea {
		return textContent("%s, %d×%d px: too large to read", r.mediaType, config.Width, config.Height)
	}
	if max(config.Width, config.Height) <= sandboxImageMaxPixels && len(r.data) <= sandboxImageResultBytes {
		return append(textContent("%s, %d×%d px", r.mediaType, config.Width, config.Height), &mcp.ImageContent{Meta: sandboxImageMeta, Data: r.data, MIMEType: r.mediaType})
	}
	data, size, err := fitImage(r.data)
	if err != nil {
		return textContent("binary file, %s", r.mediaType)
	}
	return append(textContent("%s, %d×%d px, shown at %d×%d", r.mediaType, config.Width, config.Height, size.X, size.Y),
		&mcp.ImageContent{Meta: sandboxImageMeta, Data: data, MIMEType: "image/jpeg"})
}

// fitImage scales an image to the size models see and encodes it as a JPEG
// within the result budget, flattening any transparency onto white.
func fitImage(data []byte) ([]byte, image.Point, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, image.Point{}, err
	}
	bounds := src.Bounds()
	scale := min(1, float64(sandboxImageMaxPixels)/float64(max(bounds.Dx(), bounds.Dy())))
	var out bytes.Buffer
	for {
		dst := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(bounds.Dx())*scale)), max(1, int(float64(bounds.Dy())*scale))))
		draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)
		for _, quality := range []int{85, 70, 55, 40} {
			out.Reset()
			if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: quality}); err != nil {
				return nil, image.Point{}, err
			}
			if out.Len() <= sandboxImageResultBytes {
				return out.Bytes(), dst.Bounds().Size(), nil
			}
		}
		// Detail too fine for compression to shrink takes fewer pixels.
		scale *= 0.75
	}
}

// linePage collects numbered lines from offset until it holds limit lines or
// the text budget, cutting overlong lines.
type linePage struct {
	offset, limit int
	line, last    int
	current, text []byte
	cut, more     bool
}

func (p *linePage) write(data []byte) error {
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			p.extend(data)
			return nil
		}
		p.extend(data[:end])
		data = data[end+1:]
		if err := p.endLine(); err != nil {
			return err
		}
	}
	return nil
}

func (p *linePage) extend(data []byte) {
	room := sandboxFileLineBytes - len(p.current)
	if len(data) > room {
		data, p.cut = data[:max(room, 0)], true
	}
	p.current = append(p.current, data...)
}

func (p *linePage) endLine() error {
	p.line++
	line, cut := p.current, p.cut
	p.current, p.cut = p.current[:0], false
	if p.line < p.offset {
		return nil
	}
	if p.line >= p.offset+p.limit || len(p.text)+len(line) > sandboxToolTextBytes {
		p.more = true
		return errReadDone
	}
	p.text = fmt.Appendf(p.text, "%d: %s", p.line, line)
	if cut {
		p.text = append(p.text, " [line cut]"...)
	}
	p.text = append(p.text, '\n')
	p.last = p.line
	return nil
}

func (p *linePage) finish() string {
	if len(p.current) > 0 && !p.more {
		_ = p.endLine()
	}
	text := strings.ToValidUTF8(string(p.text), "\uFFFD")
	switch {
	case p.more:
		return fmt.Sprintf("%s(lines %d–%d; continue with offset=%d)", text, p.offset, p.last, p.last+1)
	case p.last == 0:
		return fmt.Sprintf("(the file has %d lines)", p.line)
	}
	return text
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

// addSandboxToolContent registers a tool whose result is content for the model,
// with no structured output.
func addSandboxToolContent[In any](server *mcp.Server, name, description string, call func(context.Context, In) ([]mcp.Content, error)) {
	mcp.AddTool(server, &mcp.Tool{Name: name, Description: description}, func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, any, error) {
		content, err := call(ctx, input)
		if err != nil {
			return toolError(errors.New(serviceerrors.MessageOf(err))), nil, nil
		}
		return &mcp.CallToolResult{Content: content}, nil, nil
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
		addSandboxTool(server, "list_sandbox_templates", "Discover SandboxTemplate namespace/name references visible to you. Does not report readiness or installed programs; inspect the template or verify programs after creation.", func(ctx context.Context, in input) (output, error) {
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
	addSandboxTool(server, "create_sandbox", "Create a temporary workspace from a prepared SandboxTemplate. Retain request_id and identical inputs for lifecycle retries. Check state, operation, and expires_at before guest calls. No network egress is currently allowed; files belong under /data/workspace.", func(ctx context.Context, in sandboxCreateInput) (sandboxSummary, error) {
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
		{"get_sandbox", "Inspect state, operation, failure, and expiration. This only observes; repeat a pending lifecycle mutation to advance it.", service.Get},
		{"suspend_sandbox", "Suspend a workspace; running commands and transfers may be interrupted", service.Suspend},
		{"resume_sandbox", "Resume a workspace; previous guest process handles are no longer valid", service.Resume},
		{"delete_sandbox", "Delete a workspace and its files after retrieving needed artifacts. Retry the same deletion on transient failure; inspect state and operation to confirm completion.", service.Delete},
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
	addSandboxTool(server, "start_sandbox_process", "Start once and retain process_id. This returns before completion; use get_sandbox_process and read_sandbox_outputs next. cwd defaults to /data/workspace; command is argv, not shell text. Retrying an uncertain start may execute the command twice.", func(ctx context.Context, in sandboxStartInput) (sandboxStartOutput, error) {
		request := &guestpb.StartProcessRequest{Command: in.Command, Cwd: in.CWD, Env: in.Env}
		result, err := service.StartProcess(ctx, in.SandboxID, request)
		return sandboxStartOutput{ProcessID: result.GetProcessId()}, err
	})
	addSandboxTool(server, "get_sandbox_process", "Inspect process status. exit_code is meaningful only for COMPLETED, FAILED, or TERMINATED, not RUNNING. Read outputs and retrieve artifacts after completion.", func(ctx context.Context, in sandboxProcessInput) (sandboxProcessOutput, error) {
		result, err := service.GetProcess(ctx, in.SandboxID, &guestpb.GetProcessRequest{ProcessId: in.ProcessID})
		return sandboxProcessOutput{ProcessID: result.GetProcessId(), Status: result.GetStatus().String(), ExitCode: result.GetExitCode()}, err
	})
	addSandboxTool(server, "kill_sandbox_process", "Terminate a sandbox process", func(ctx context.Context, in sandboxProcessInput) (sandboxProcessOutput, error) {
		result, err := service.KillProcess(ctx, in.SandboxID, &guestpb.KillProcessRequest{ProcessId: in.ProcessID})
		return sandboxProcessOutput{ProcessID: in.ProcessID, ExitCode: result.GetExitCode()}, err
	})
	addSandboxTool(server, "read_sandbox_outputs", "Read currently available stdout/stderr as base64, up to 1 MiB combined. Pass both returned byte offsets to continue. This does not wait for completion; check get_sandbox_process and read again after it finishes.", func(ctx context.Context, in sandboxOutputsInput) (sandboxOutputsOutput, error) {
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
	addSandboxToolContent(server, "read_sandbox_file", "Read a file. Text comes back as numbered lines from offset, up to limit lines or 32 KiB, with the offset to continue from. PNG, JPEG, GIF and WebP images up to 16 MiB come back as images you can see, scaled to at most 2000 px on a side. Other files are described rather than returned. Paths are absolute or relative to /data/workspace.", func(ctx context.Context, in sandboxReadInput) ([]mcp.Content, error) {
		reader := fileReader{page: linePage{offset: max(in.Offset, 1), limit: sandboxFileLines}}
		if in.Limit > 0 {
			reader.page.limit = in.Limit
		}
		err := service.ReadFile(ctx, in.SandboxID, &guestpb.ReadFileRequest{Path: in.Path}, func(chunk *guestpb.FileChunk) error {
			return reader.write(chunk.Data)
		})
		if err != nil && !errors.Is(err, errReadDone) {
			return nil, err
		}
		return reader.content(), nil
	})
	addSandboxTool(server, "write_sandbox_file", "Replace a file with decoded base64 data, up to 1 MiB. Paths are absolute or relative to /data/workspace. Verify bytes_written; interrupted writes may leave partial files. There is no append or offset input.", func(ctx context.Context, in sandboxWriteInput) (sandboxWriteOutput, error) {
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
