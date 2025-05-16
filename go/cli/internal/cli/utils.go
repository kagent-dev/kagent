package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/jedib0t/go-pretty/v6/table"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

func CheckServerConnection(client *autogen_client.Client) error {
	// Only check if we have a valid client
	if client == nil {
		return fmt.Errorf("Error connecting to server. Please run 'install' command first.")
	}

	_, err := client.GetVersion()
	if err != nil {
		return fmt.Errorf("Error connecting to server. Please run 'install' command first.")
	}
	return nil
}

type portForward struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

func newPortForward(ctx context.Context, cfg *config.Config) *portForward {
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, "kubectl", "-n", "kagent", "port-forward", "service/kagent", "8081:8081")
	// Error connecting to server, port-forward the server
	go func() {
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
			os.Exit(1)
		}
	}()
	return &portForward{
		cmd:    cmd,
		cancel: cancel,
	}
}

func (p *portForward) Stop() {
	p.cancel()
	if err := p.cmd.Wait(); err != nil {
		fmt.Fprintf(os.Stderr, "Error waiting for port-forward to exit: %v\n", err)
	}
}

type Event interface {
	GetType() string
}

type BaseEvent struct {
	Type string `json:"type"`
}

func (e *BaseEvent) GetType() string {
	return e.Type
}

type BaseChatMessage struct {
	BaseEvent
	Source      string                      `json:"source"`
	Metadata    map[string]string           `json:"metadata"`
	ModelsUsage *autogen_client.ModelsUsage `json:"models_usage"`
}

type TextMessage struct {
	BaseChatMessage
	Content string `json:"content"`
}

type ModelClientStreamingChunkEvent struct {
	BaseChatMessage
	Content string `json:"content"`
}
type FunctionCall struct {
	ID        string `json:"id"`
	Arguments string `json:"arguments"`
	Name      string `json:"name"`
}
type ToolCallRequestEvent struct {
	BaseChatMessage
	Content []FunctionCall `json:"content"`
}

type FunctionExecutionResult struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
}

type ToolCallExecutionEvent struct {
	BaseChatMessage
	Content []FunctionExecutionResult `json:"content"`
}

type MemoryQueryEvent struct {
	BaseChatMessage
	Content []map[string]interface{} `json:"content"`
}

const (
	TextMessageLabel                    = "TextMessage"
	ToolCallRequestEventLabel           = "ToolCallRequestEvent"
	ToolCallExecutionEventLabel         = "ToolCallExecutionEvent"
	StopMessageLabel                    = "StopMessage"
	HandoffMessageLabel                 = "HandoffMessage"
	ModelClientStreamingChunkEventLabel = "ModelClientStreamingChunkEvent"
	LLMCallEventMessageLabel            = "LLMCallEventMessage"
	MemoryQueryEventLabel               = "MemoryQueryEvent"
)

func ParseEvent(event []byte) (Event, error) {
	var baseEvent BaseEvent
	if err := json.Unmarshal(event, &baseEvent); err != nil {
		return nil, err
	}

	switch baseEvent.Type {
	case TextMessageLabel:
		var textMessage TextMessage
		if err := json.Unmarshal(event, &textMessage); err != nil {
			return nil, err
		}
		return &textMessage, nil
	case ModelClientStreamingChunkEventLabel:
		var modelClientStreamingChunkEvent ModelClientStreamingChunkEvent
		if err := json.Unmarshal(event, &modelClientStreamingChunkEvent); err != nil {
			return nil, err
		}
		return &modelClientStreamingChunkEvent, nil
	case ToolCallRequestEventLabel:
		var toolCallRequestEvent ToolCallRequestEvent
		if err := json.Unmarshal(event, &toolCallRequestEvent); err != nil {
			return nil, err
		}
		return &toolCallRequestEvent, nil
	case ToolCallExecutionEventLabel:
		var toolCallExecutionEvent ToolCallExecutionEvent
		if err := json.Unmarshal(event, &toolCallExecutionEvent); err != nil {
			return nil, err
		}
		return &toolCallExecutionEvent, nil
	case MemoryQueryEventLabel:
		var memoryQueryEvent MemoryQueryEvent
		if err := json.Unmarshal(event, &memoryQueryEvent); err != nil {
			return nil, err
		}
		return &memoryQueryEvent, nil
	default:
		return nil, fmt.Errorf("unknown event type: %s", baseEvent.Type)
	}
}

func StreamEvents(ch <-chan *autogen_client.SseEvent, usage *autogen_client.ModelsUsage, verbose bool) {
	// Tool call requests and executions are sent as separate messages, but we should print them together
	// so if we receive a tool call request, we buffer it until we receive the corresponding tool call execution
	// We only need to buffer one request and one execution at a time
	var bufferedToolCallRequest *ToolCallRequestEvent
	// This is a map of agent source to whether we are currently streaming from that agent
	// If we are then we don't want to print the whole TextMessage, but only the content of the ModelStreamingEvent
	streaming := map[string]bool{}
	for event := range ch {
		ev, err := ParseEvent(event.Data)
		if err != nil {
			// TODO: verbose logging
			continue
		}
		switch typed := ev.(type) {
		case *TextMessage:
			// c.Println(typed.Content)
			usage.Add(typed.ModelsUsage)
			// If we are streaming from this agent, don't print the whole TextMessage, but only the content of the ModelStreamingEvent
			if streaming[typed.Source] {
				fmt.Fprintln(os.Stdout)
				continue
			}
			// Do not re-print the user's input, or system message asking for input
			if typed.Source == "user" || typed.Source == "system" {
				continue
			}
			fmt.Fprintf(os.Stdout, "%s: %s\n", config.BoldYellow("Event Type"), "TextMessage")
			fmt.Fprintf(os.Stdout, "%s: %s\n", config.BoldGreen("Source"), typed.Source)
			fmt.Fprintln(os.Stdout)
			fmt.Fprintln(os.Stdout, typed.Content)
			fmt.Fprintln(os.Stdout, "----------------------------------")
			fmt.Fprintln(os.Stdout)
		case *ModelClientStreamingChunkEvent:
			usage.Add(typed.ModelsUsage)
			streaming[typed.Source] = true
			fmt.Fprintf(os.Stdout, "%s", typed.Content)
		case *ToolCallRequestEvent:
			bufferedToolCallRequest = typed
		case *ToolCallExecutionEvent:
			if bufferedToolCallRequest == nil {
				fmt.Fprintf(os.Stderr, "Received tool call execution before request: %v\n", typed)
				continue
			}
			usage.Add(typed.ModelsUsage)
			fmt.Fprintf(os.Stdout, "%s: %s\n", config.BoldYellow("Event Type"), "ToolCall(s)")
			fmt.Fprintf(os.Stdout, "%s: %s\n", config.BoldGreen("Source"), typed.Source)
			if verbose {
				// For each function execution, find the corresponding tool call request and print them together
				for i, functionExecution := range typed.Content {
					for _, functionRequest := range bufferedToolCallRequest.Content {
						if functionExecution.CallID == functionRequest.ID {
							fmt.Fprintln(os.Stdout)
							fmt.Fprintln(os.Stdout, "++++++++")
							fmt.Fprintf(os.Stdout, "Tool Call %d: (id: %s)\n", i, functionRequest.ID)
							fmt.Fprintln(os.Stdout)
							fmt.Fprintf(os.Stdout, "%s(%s)\n", functionRequest.Name, functionRequest.Arguments)
							fmt.Fprintln(os.Stdout)
							fmt.Fprintln(os.Stdout, functionExecution.Content)
							fmt.Fprintln(os.Stdout, "++++++++")
							fmt.Fprintln(os.Stdout)
						}
					}
				}
			} else {
				tw := table.NewWriter()
				tw.AppendHeader(table.Row{"#", "Name", "Arguments"})
				for idx, functionRequest := range bufferedToolCallRequest.Content {
					tw.AppendRow(table.Row{idx, functionRequest.Name, functionRequest.Arguments})
				}
				fmt.Fprintln(os.Stdout, tw.Render())
			}

			fmt.Fprintln(os.Stdout, "----------------------------------")
			fmt.Fprintln(os.Stdout)
			bufferedToolCallRequest = nil
		}
	}
}
