package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/abiosoft/readline"
	"github.com/briandowns/spinner"
	"github.com/fatih/color"
	"github.com/gorilla/websocket"
	"github.com/jedib0t/go-pretty/v6/table"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
)

// Config holds the WebSocket client configuration
type Config struct {
	Origin  string // WebSocket origin header
	Verbose bool   // Whether to print verbose output
}

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		Origin:  "http://localhost:8000",
		Verbose: false,
	}
}

// Client handles the WebSocket connection and message processing
type Client struct {
	conn   *websocket.Conn
	done   chan struct{}
	config Config
}

// NewClient creates a new WebSocket client and establishes connection
func NewClient(wsURL string, runID string, config Config) (*Client, error) {
	// Set the required headers for the WebSocket connection
	headers := http.Header{}
	headers.Add("Origin", config.Origin)

	// Create dialer with debug logging
	dialer := websocket.Dialer{
		HandshakeTimeout:  10 * time.Second,
		EnableCompression: true,
	}

	conn, _, err := dialer.Dial(wsURL+"/runs/"+runID, headers)
	if err != nil {
		return nil, fmt.Errorf("websocket connection failed: %v", err)
	}

	return &Client{
		conn:   conn,
		done:   make(chan struct{}),
		config: config,
	}, nil
}

type Shell interface {
	ReadLineErr() (string, error)
	// Println prints to output and ends with newline character.
	Println(val ...interface{})
	// Printf prints to output using string format.
	Printf(format string, val ...interface{})
}

// StartInteractive initiates the interactive session with the server
func (c *Client) StartInteractive(ctx context.Context, shell Shell, team autogen_client.Team, task string) error {
	defer c.conn.Close()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// Send initial start message
	startMsg := StartMessage{
		Type:       MessageTypeStart,
		Task:       task,
		TeamConfig: team.Component,
	}

	if err := c.conn.WriteJSON(startMsg); err != nil {
		return fmt.Errorf("failed to send start message: %v", err)
	}

	go c.handleMessages(shell)

	select {
	case <-ctx.Done():
		fmt.Printf("\nContext cancelled (%s). Stopping task...", ctx.Err())
		stopMsg := StopMessage{
			Type:   MessageTypeStop,
			Reason: "Cancelled by user",
		}
		if err := c.conn.WriteJSON(stopMsg); err != nil {
			fmt.Fprintf(os.Stderr, "Error sending stop message: %v\n", err)
		}
		select {
		case <-c.done:
		case <-time.After(time.Second):
		}
		return nil
	case <-c.done:
		return nil
	case <-interrupt:
		stopMsg := StopMessage{
			Type:   MessageTypeStop,
			Reason: "Cancelled by user",
		}
		if err := c.conn.WriteJSON(stopMsg); err != nil {
			fmt.Fprintf(os.Stderr, "Error sending stop message: %v\n", err)
		}
		return nil
	}
}

// Yes, this is AI generated, and so is this comment.
var thinkingVerbs = []string{"thinking", "processing", "mulling over", "pondering", "reflecting", "evaluating", "analyzing", "synthesizing", "interpreting", "inferring", "deducing", "reasoning", "evaluating", "synthesizing", "interpreting", "inferring", "deducing", "reasoning"}

func getThinkingVerb() string {
	return thinkingVerbs[rand.Intn(len(thinkingVerbs))]
}

func (c *Client) handleMessages(shell Shell) {
	defer close(c.done)
	bold_yellow := color.New(color.FgYellow, color.Bold).SprintFunc()
	bold_green := color.New(color.FgGreen, color.Bold).SprintFunc()

	// Tool call requests and executions are sent as separate messages, but we should print them together
	// so if we receive a tool call request, we buffer it until we receive the corresponding tool call execution
	// We only need to buffer one request and one execution at a time
	var bufferedToolCallRequest *ToolCallRequest
	// This is a map of agent source to whether we are currently streaming from that agent
	// If we are then we don't want to print the whole TextMessage, but only the content of the ModelStreamingEvent
	streaming := map[string]bool{}
	title := getThinkingVerb()
	s := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	s.Suffix = " " + title
	s.Start()
	defer s.Stop()

	for {
		var msg BaseWebSocketMessage

		err := c.conn.ReadJSON(&msg)
		if err != nil {
			shell.Printf("Error reading message: %v\n", err)
			return
		}
		switch msg.Type {
		case MessageTypeError:
			shell.Printf("Error: %s\n", msg.Error)
			return

		case MessageTypeMessage:
			mapStructure := &map[string]string{}
			_ = json.Unmarshal(msg.Data, mapStructure)
			typeStr := (*mapStructure)["type"]
			contentType := ContentType(typeStr)

			switch contentType {
			case ContentTypeText:
				var textMessage TextMessage
				if err := json.Unmarshal(msg.Data, &textMessage); err != nil {
					shell.Printf("Error parsing message data: %v\n", err)
					continue
				}
				// If we are streaming from this agent, don't print the whole TextMessage, but only the content of the ModelStreamingEvent
				if streaming[textMessage.Source] {
					// reset buffer?
					shell.Println()
					continue
				}
				if s.Active() {
					s.Stop()
				}
				// Do not re-print the user's input, or system message asking for input
				if textMessage.Source == "user" || textMessage.Source == "system" {
					s.Start()
					continue
				}
				shell.Printf("%s: %s\n", bold_yellow("Event Type"), contentType)
				shell.Printf("%s: %s\n", bold_green("Source"), textMessage.Source)
				shell.Println()
				shell.Println(textMessage.Content)
				shell.Println("----------------------------------")
				shell.Println()
				s.Suffix = " " + getThinkingVerb()
				s.Start()
			case ContentTypeToolCallRequest:
				var toolCallRequest ToolCallRequest
				if err := json.Unmarshal(msg.Data, &toolCallRequest); err != nil {
					shell.Printf("Error parsing message data: %v\n", err)
					continue
				}
				// s.Suffix = " " + "calling tools"
				// Buffer the tool call request until we receive the corresponding tool call execution
				bufferedToolCallRequest = &toolCallRequest

			case ContentTypeToolCallExecution:
				var toolCallExecution ToolCallExecution
				if err := json.Unmarshal(msg.Data, &toolCallExecution); err != nil {
					shell.Printf("Error parsing message data: %v\n", err)
					continue
				}

				if s.Active() {
					s.Stop()
				}

				shell.Printf("%s: %s\n", bold_yellow("Event Type"), "ToolCall(s)")
				shell.Printf("%s: %s\n", bold_green("Source"), toolCallExecution.Source)

				if c.config.Verbose {
					// For each function execution, find the corresponding tool call request and print them together
					for i, functionExecution := range toolCallExecution.Content {
						for _, functionRequest := range bufferedToolCallRequest.Content {
							if functionExecution.CallID == functionRequest.ID {
								shell.Println()
								shell.Println("++++++++")
								shell.Printf("Tool Call %d: (id: %s)\n", i, functionRequest.ID)
								shell.Println()
								shell.Printf("%s(%s)\n", functionRequest.Name, functionRequest.Arguments)
								shell.Println()
								shell.Println(functionExecution.Content)
								shell.Println("++++++++")
								shell.Println()
							}
						}
					}
				} else {
					tw := table.NewWriter()
					tw.AppendHeader(table.Row{"#", "Name", "Arguments"})
					for idx, functionRequest := range bufferedToolCallRequest.Content {
						tw.AppendRow(table.Row{idx, functionRequest.Name, functionRequest.Arguments})
					}
					shell.Println(tw.Render())
				}

				shell.Println("----------------------------------")
				shell.Println()

				s.Suffix = " " + getThinkingVerb()
				s.Start()
				// Reset the buffered tool call request now that we've received the execution
				bufferedToolCallRequest = nil
			case ContentTypeModelStreaming:
				if s.Active() {
					s.Stop()
				}
				var modelStreaming ModelStreamingEvent
				if err := json.Unmarshal(msg.Data, &modelStreaming); err != nil {
					shell.Printf("Error parsing message data: %v\n", err)
					continue
				}
				streaming[modelStreaming.Source] = true
				shell.Printf(modelStreaming.Content)
			}

		case MessageTypeInputRequest:
			go func() {
				// TODO: properly handle this error
				if err := c.handleUserInput(shell, msg.Data); err != nil {
					shell.Printf("Error handling input: %v\n", err)
					return
				}
			}()

		case MessageTypeResult, MessageTypeCompletion:
			var msgResult CompletionMessage
			if err := json.Unmarshal(msg.Data, &msgResult); err != nil {
				shell.Printf("Error parsing message data: %v\n", err)
				continue
			}

			shell.Printf("Closing session, thank you :)")
			// shell.Printf("\n(%s) Task completed:\n%s", msgResult.Status, msgResult.Data)
			return
		}

	}
}

func (c *Client) handleUserInput(shell Shell, msg json.RawMessage) error {
	var inputRequest InputRequestMessage
	if err := json.Unmarshal(msg, &inputRequest); err != nil {
		return fmt.Errorf("error parsing input request: %v", err)
	}
	for {
		input, err := shell.ReadLineErr()
		if err != nil {
			if errors.Is(err, readline.ErrInterrupt) {
				// Send stop message when ctrl-c is pressed
				stopMsg := StopMessage{
					Type:   MessageTypeStop,
					Reason: "Ended by user",
				}
				return c.conn.WriteJSON(stopMsg)
			}
			return fmt.Errorf("error reading input: %v", err)
		}
		if input == "exit" {
			stopMsg := StopMessage{
				Type:   MessageTypeStop,
				Reason: "Ended by user",
			}
			return c.conn.WriteJSON(stopMsg)
		}
		if input == "help" {
			shell.Println("Available commands:")
			shell.Println("  help - Show this help")
			shell.Println("  exit - Exit the program")
			continue
		}
		inputMsg := InputResponseMessage{
			Type:     MessageTypeInputResponse,
			Response: input,
		}
		return c.conn.WriteJSON(inputMsg)
	}
}
