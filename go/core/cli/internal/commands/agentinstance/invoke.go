package agentinstance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	clia2a "github.com/kagent-dev/kagent/go/core/cli/internal/a2a"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/spf13/cobra"
)

var errTruncatedA2AStream = errors.New("a2a stream ended before returning a final result")

type InvokeCfg struct {
	OutputFormat  string
	Task          string
	File          string
	AgentInstance string
	Stream        bool
	Token         string
}

func runInvoke(
	ctx context.Context,
	options connection.Options,
	cfg *InvokeCfg,
	in io.Reader,
	out io.Writer,
) (err error) {
	format, err := clioutput.Parse(cfg.OutputFormat)
	if err != nil {
		return err
	}
	task, err := readInvokeTask(cfg, in)
	if err != nil {
		return err
	}
	instanceID, err := uuid.Parse(cfg.AgentInstance)
	if err != nil {
		return fmt.Errorf("invalid AgentInstance ID %q: %w", cfg.AgentInstance, err)
	}
	if strings.ContainsAny(cfg.Token, " \t\r\n") {
		return errors.New("model API key must not contain whitespace")
	}

	session, err := connection.Open(ctx, options)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, session.Close())
	}()
	a2aClient, err := session.Client.A2A.ForAgentInstance(ctx, session.Namespace, instanceID.String())
	if err != nil {
		return fmt.Errorf("create AgentInstance A2A client: %w", err)
	}

	request := newInvokeRequest(task, instanceID.String())
	ctx = withModelToken(ctx, cfg.Token)

	if cfg.Stream {
		return invokeStreaming(ctx, a2aClient, request, format, out)
	}
	return invokeNonStreaming(ctx, a2aClient, request, format, out)
}

func newInvokeRequest(task, instanceID string) *a2atype.SendMessageRequest {
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(task))
	message.ContextID = instanceID
	return &a2atype.SendMessageRequest{Message: message}
}

func withModelToken(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return a2aclient.AttachServiceParams(ctx, a2aclient.ServiceParams{"authorization": {"Bearer " + token}})
}

func readInvokeTask(cfg *InvokeCfg, in io.Reader) (string, error) {
	if (cfg.Task == "") == (cfg.File == "") {
		return "", errors.New("exactly one of --task or --file is required")
	}
	if cfg.Task != "" {
		if strings.TrimSpace(cfg.Task) == "" {
			return "", errors.New("task is empty")
		}
		return cfg.Task, nil
	}

	var (
		content []byte
		err     error
	)
	if cfg.File == "-" {
		content, err = io.ReadAll(in)
	} else {
		content, err = os.ReadFile(cfg.File)
	}
	if err != nil {
		return "", fmt.Errorf("read task from %q: %w", cfg.File, err)
	}
	if strings.TrimSpace(string(content)) == "" {
		return "", errors.New("task is empty")
	}
	return string(content), nil
}

func invokeNonStreaming(
	ctx context.Context,
	client *a2aclient.Client,
	request *a2atype.SendMessageRequest,
	format clioutput.Format,
	out io.Writer,
) error {
	result, err := client.SendMessage(ctx, request)
	if err != nil {
		return fmt.Errorf("invoke AgentInstance: %w", err)
	}
	if err := writeSendResult(out, format, result); err != nil {
		return err
	}
	return sendResultError(result)
}

func invokeStreaming(
	ctx context.Context,
	client *a2aclient.Client,
	request *a2atype.SendMessageRequest,
	format clioutput.Format,
	out io.Writer,
) error {
	var onEvent func(a2atype.Event, a2atype.SendMessageResult) error
	var tableWriter *tableStreamWriter
	if format == clioutput.FormatJSON {
		onEvent = func(event a2atype.Event, _ a2atype.SendMessageResult) error {
			return writeStreamEvent(out, event)
		}
	} else {
		tableWriter = &tableStreamWriter{w: out}
		onEvent = tableWriter.Write
	}

	result, streamErr := consumeA2AStream(client.SendStreamingMessage(ctx, request), onEvent)
	return finishInvokeStream(tableWriter, result, streamErr)
}

func finishInvokeStream(tableWriter *tableStreamWriter, result a2atype.SendMessageResult, streamErr error) error {
	if tableWriter != nil {
		streamErr = errors.Join(streamErr, tableWriter.Finish(result))
	}
	if streamErr != nil {
		return fmt.Errorf("invoke AgentInstance stream: %w", streamErr)
	}
	return sendResultError(result)
}

func writeStreamEvent(w io.Writer, event a2atype.Event) error {
	response, err := pbconv.ToProtoStreamResponse(event)
	if err != nil {
		return fmt.Errorf("convert A2A event to protobuf: %w", err)
	}
	return clioutput.WriteProto(w, response)
}

func consumeA2AStream(
	stream iter.Seq2[a2atype.Event, error],
	onEvent func(a2atype.Event, a2atype.SendMessageResult) error,
) (a2atype.SendMessageResult, error) {
	assembler := &clia2a.Assembler{}
	for event, streamErr := range stream {
		if event != nil {
			if err := assembler.Apply(event); err != nil {
				return assembler.Result(), err
			}
			if err := onEvent(event, assembler.Result()); err != nil {
				return assembler.Result(), err
			}
		}
		if streamErr != nil {
			return assembler.Result(), streamErr
		}
		if event == nil {
			return assembler.Result(), errors.New("a2a stream returned an empty event")
		}
	}
	if !assembler.Complete() {
		return assembler.Result(), errTruncatedA2AStream
	}
	return assembler.Result(), nil
}

type tableStreamWriter struct {
	w    io.Writer
	text string
}

func (w *tableStreamWriter) Write(_ a2atype.Event, result a2atype.SendMessageResult) error {
	text := sendResultText(result)
	if text == w.text {
		return nil
	}

	delta, extends := strings.CutPrefix(text, w.text)
	if !extends {
		delta = text
		if w.text != "" {
			delta = "\n" + text
		}
	}
	if _, err := io.WriteString(w.w, delta); err != nil {
		return fmt.Errorf("write invoke stream: %w", err)
	}
	w.text = text
	return nil
}

func (w *tableStreamWriter) Finish(result a2atype.SendMessageResult) error {
	if w.text != "" && !strings.HasSuffix(w.text, "\n") {
		if _, err := io.WriteString(w.w, "\n"); err != nil {
			return fmt.Errorf("write invoke stream: %w", err)
		}
	}
	return writeTableContinuation(w.w, result)
}

func writeSendResult(w io.Writer, format clioutput.Format, result a2atype.SendMessageResult) error {
	if format == clioutput.FormatTable {
		return writeTableResult(w, result)
	}
	response, err := pbconv.ToProtoSendMessageResponse(result)
	if err != nil {
		return fmt.Errorf("convert A2A result to protobuf: %w", err)
	}
	return clioutput.WriteProto(w, response)
}

func writeTableResult(w io.Writer, result a2atype.SendMessageResult) error {
	text := sendResultText(result)
	if text != "" {
		if _, err := io.WriteString(w, text); err != nil {
			return fmt.Errorf("write invoke output: %w", err)
		}
		if !strings.HasSuffix(text, "\n") {
			if _, err := io.WriteString(w, "\n"); err != nil {
				return fmt.Errorf("write invoke output: %w", err)
			}
		}
	}

	return writeTableContinuation(w, result)
}

func writeTableContinuation(w io.Writer, result a2atype.SendMessageResult) error {
	task, ok := result.(*a2atype.Task)
	if !ok {
		return nil
	}
	var continuation string
	switch task.Status.State {
	case a2atype.TaskStateInputRequired:
		continuation = "Input required to continue this AgentInstance.\n"
	case a2atype.TaskStateAuthRequired:
		continuation = "Authentication required to continue this AgentInstance.\n"
	}
	if continuation != "" {
		if _, err := io.WriteString(w, continuation); err != nil {
			return fmt.Errorf("write invoke continuation: %w", err)
		}
	}
	return nil
}

func sendResultText(result a2atype.SendMessageResult) string {
	switch result := result.(type) {
	case *a2atype.Message:
		return messageText(result)
	case *a2atype.Task:
		groups := make([]string, 0, len(result.Artifacts)+1)
		if text := messageText(result.Status.Message); text != "" {
			groups = append(groups, text)
		}
		for _, artifact := range result.Artifacts {
			if artifact == nil {
				continue
			}
			if text := clia2a.PartsText(artifact.Parts); text != "" {
				groups = append(groups, text)
			}
		}
		return strings.Join(groups, "\n")
	default:
		return ""
	}
}

func messageText(message *a2atype.Message) string {
	if message == nil {
		return ""
	}
	return clia2a.PartsText(message.Parts)
}

func sendResultError(result a2atype.SendMessageResult) error {
	task, ok := result.(*a2atype.Task)
	if !ok {
		if result == nil {
			return errors.New("a2a invocation returned no result")
		}
		return nil
	}
	switch task.Status.State {
	case a2atype.TaskStateCompleted, a2atype.TaskStateInputRequired, a2atype.TaskStateAuthRequired:
		return nil
	case a2atype.TaskStateFailed, a2atype.TaskStateRejected, a2atype.TaskStateCanceled:
		return fmt.Errorf("AgentInstance task %s ended in %s", task.ID, task.Status.State)
	default:
		return fmt.Errorf("AgentInstance task %s returned before reaching a final state: %s", task.ID, task.Status.State)
	}
}

// NewInvokeCmd constructs the AgentInstance invoke command.
func NewInvokeCmd() *cobra.Command {
	cfg := &InvokeCfg{}
	cmd := &cobra.Command{
		Use:     "invoke",
		Short:   "Invoke an AgentInstance",
		Long:    `Invoke an existing AgentInstance through the A2A API.`,
		Args:    cobra.NoArgs,
		Example: `kagent invoke --agent-instance 8bd650a8-9775-488f-8bc1-0d52bf7bdcab --task "Get all the pods"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := connection.OptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			format, err := clioutput.FromCommand(cmd)
			if err != nil {
				return err
			}
			cfg.OutputFormat = format
			return runInvoke(cmd.Context(), options, cfg, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&cfg.AgentInstance, "agent-instance", "", "AgentInstance ID")
	cmd.Flags().StringVarP(&cfg.Task, "task", "t", "", "Task text")
	cmd.Flags().StringVarP(&cfg.File, "file", "f", "", "Read task text from a file or - for stdin")
	cmd.Flags().BoolVarP(&cfg.Stream, "stream", "S", false, "Stream the response")
	cmd.Flags().StringVar(&cfg.Token, "token", "", "Model API key passed through as an A2A Bearer token")
	_ = cmd.MarkFlagRequired("agent-instance")
	cmd.MarkFlagsOneRequired("task", "file")
	cmd.MarkFlagsMutuallyExclusive("task", "file")
	return cmd
}
