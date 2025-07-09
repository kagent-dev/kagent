package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	autogen_client "github.com/kagent-dev/kagent/go/internal/autogen/client"
	"github.com/kagent-dev/kagent/go/pkg/client"
	"github.com/kagent-dev/kagent/go/pkg/sse"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func CheckServerConnection(client *client.ClientSet) error {
	// Only check if we have a valid client
	if client == nil {
		return fmt.Errorf("Error connecting to server. Please run 'install' command first.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	_, err := client.Version.GetVersion(ctx)
	if err != nil {
		return fmt.Errorf("Error connecting to server. Please run 'install' command first.")
	}
	return nil
}

type portForward struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

func NewPortForward(ctx context.Context, cfg *config.Config) *portForward {
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, "kubectl", "-n", cfg.Namespace, "port-forward", "service/kagent", "8081:8081")
	// Error connecting to server, port-forward the server
	go func() {
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
			os.Exit(1)
		}
	}()

	client := client.New(cfg.APIURL)
	// Try to connect 5 times
	for i := 0; i < 5; i++ {
		if err := CheckServerConnection(client); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	return &portForward{
		cmd:    cmd,
		cancel: cancel,
	}
}

func (p *portForward) Stop() {
	p.cancel()
	if err := p.cmd.Wait(); err != nil {
		if !strings.Contains(err.Error(), "signal: killed") && !strings.Contains(err.Error(), "exit status 1") {
			fmt.Fprintf(os.Stderr, "Error waiting for port-forward to exit: %v\n", err)
		}
	}
}

func StreamEvents(ch <-chan *sse.Event, usage *autogen_client.ModelsUsage, verbose bool) {
	// Tool call requests and executions are sent as separate messages, but we should print them together
	// so if we receive a tool call request, we buffer it until we receive the corresponding tool call execution
	// We only need to buffer one request and one execution at a time
	var bufferedToolCallRequest *autogen_client.ToolCallRequestEvent
	// This is a map of agent source to whether we are currently streaming from that agent
	// If we are then we don't want to print the whole TextMessage, but only the content of the ModelStreamingEvent
	streaming := map[string]bool{}
	for event := range ch {
		ev, err := autogen_client.ParseEvent(event.Data)
		if err != nil {
			// TODO: verbose logging
			continue
		}
		switch typed := ev.(type) {
		case *autogen_client.TextMessage:
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
			if verbose {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(typed); err != nil {
					fmt.Fprintf(os.Stderr, "Error encoding event: %v\n", err)
					continue
				}
			} else {
				fmt.Fprintf(os.Stdout, "%s: %s\n", config.BoldYellow("Event Type"), "TextMessage")
				fmt.Fprintf(os.Stdout, "%s: %s\n", config.BoldGreen("Source"), typed.Source)
				fmt.Fprintln(os.Stdout)
				fmt.Fprintln(os.Stdout, typed.Content)
				fmt.Fprintln(os.Stdout, "----------------------------------")
				fmt.Fprintln(os.Stdout)
			}
		case *autogen_client.ModelClientStreamingChunkEvent:
			usage.Add(typed.ModelsUsage)
			streaming[typed.Source] = true
			if verbose {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(typed); err != nil {
					fmt.Fprintf(os.Stderr, "Error encoding event: %v\n", err)
					continue
				}
			} else {
				fmt.Fprintf(os.Stdout, "%s", typed.Content)
			}
		case *autogen_client.ToolCallRequestEvent:
			bufferedToolCallRequest = typed
		case *autogen_client.ToolCallExecutionEvent:
			if bufferedToolCallRequest == nil {
				fmt.Fprintf(os.Stderr, "Received tool call execution before request: %v\n", typed)
				continue
			}
			usage.Add(typed.ModelsUsage)
			if verbose {
				enc := json.NewEncoder(os.Stdout)
				out := map[string]interface{}{
					"request":   bufferedToolCallRequest,
					"execution": typed,
				}
				enc.SetIndent("", "  ")
				if err := enc.Encode(out); err != nil {
					fmt.Fprintf(os.Stderr, "Error encoding event: %v\n", err)
					continue
				}
			} else {
				fmt.Fprintf(os.Stdout, "%s: %s\n", config.BoldYellow("Event Type"), "ToolCall(s)")
				fmt.Fprintf(os.Stdout, "%s: %s\n", config.BoldGreen("Source"), typed.Source)
				tw := table.NewWriter()
				tw.AppendHeader(table.Row{"#", "Name", "Arguments"})
				for idx, functionRequest := range bufferedToolCallRequest.Content {
					tw.AppendRow(table.Row{idx, functionRequest.Name, functionRequest.Arguments})
				}
				fmt.Fprintln(os.Stdout, tw.Render())
			}

			if !verbose {
				fmt.Fprintln(os.Stdout, "----------------------------------")
				fmt.Fprintln(os.Stdout)
			}

			bufferedToolCallRequest = nil
		}
	}
}

func StreamA2AEvents(ch <-chan protocol.StreamingMessageEvent, verbose bool) {
	for event := range ch {
		if verbose {
			json, err := event.MarshalJSON()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error marshaling A2A event: %v\n", err)
				continue
			}
			fmt.Fprintf(os.Stdout, "%+v\n", string(json))
		} else {
			json, err := event.MarshalJSON()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error marshaling A2A event: %v\n", err)
				continue
			}
			fmt.Fprintf(os.Stdout, "%+v\n", string(json))
		}
	}
	fmt.Fprintln(os.Stdout) // Add a newline after streaming is complete
}

func startPortForward(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	a2aPortFwdCmd := exec.CommandContext(ctx, "kubectl", "-n", "kagent", "port-forward", "service/kagent", "8083:8083")
	// Error connecting to server, port-forward the server
	go func() {
		if err := a2aPortFwdCmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
			os.Exit(1)
		}
	}()

	// Ensure the context is cancelled when the shell is closed
	return func() {
		cancel()
		if err := a2aPortFwdCmd.Wait(); err != nil {
			// These 2 errors are expected
			if !strings.Contains(err.Error(), "signal: killed") && !strings.Contains(err.Error(), "exec: not started") {
				fmt.Fprintf(os.Stderr, "Error waiting for port-forward to exit: %v\n", err)
			}
		}
	}
}
