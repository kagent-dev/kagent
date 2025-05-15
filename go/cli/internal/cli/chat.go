package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"slices"

	"github.com/abiosoft/ishell/v2"
	"github.com/abiosoft/readline"
	"github.com/jedib0t/go-pretty/v6/table"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/spf13/pflag"
)

const (
	sessionCreateNew = "[New Session]"
)

func ChatCmd(c *ishell.Context) {
	verbose := false
	var sessionName string
	flagSet := pflag.NewFlagSet(c.RawArgs[0], pflag.ContinueOnError)
	flagSet.BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	flagSet.StringVarP(&sessionName, "session", "s", "", "Session name to use")
	if err := flagSet.Parse(c.Args); err != nil {
		c.Printf("Failed to parse flags: %v\n", err)
		return
	}

	cfg := config.GetCfg(c)
	client := config.GetClient(c)

	var team *autogen_client.Team
	if len(flagSet.Args()) > 0 {
		teamName := flagSet.Args()[0]
		var err error
		team, err = client.GetTeam(teamName, cfg.UserID)
		if err != nil {
			c.Println(err)
			return
		}
	}
	// If team is not found or not passed as an argument, prompt the user to select from available teams
	if team == nil {
		c.Printf("Please select from available teams.\n")
		// Get the teams based on the input + userID
		teams, err := client.ListTeams(cfg.UserID)
		if err != nil {
			c.Println(err)
			return
		}

		if len(teams) == 0 {
			c.Println("No teams found, please create one via the web UI or CRD before chatting.")
			return
		}

		teamNames := make([]string, len(teams))
		for i, team := range teams {
			if team.Component.Label == "" {
				continue
			}
			teamNames[i] = team.Component.Label
		}

		selectedTeamIdx := c.MultiChoice(teamNames, "Select an agent:")
		team = teams[selectedTeamIdx]
	}

	sessions, err := client.ListSessions(cfg.UserID)
	if err != nil {
		c.Println(err)
		return
	}

	existingSessions := slices.Collect(Filter(slices.Values(sessions), func(session *autogen_client.Session) bool {
		return session.TeamID == team.Id
	}))

	existingSessionNames := slices.Collect(Map(slices.Values(existingSessions), func(session *autogen_client.Session) string {
		return session.Name
	}))

	// Add the new session option to the beginning of the list
	existingSessionNames = append([]string{sessionCreateNew}, existingSessionNames...)
	var selectedSessionIdx int
	if sessionName != "" {
		selectedSessionIdx = slices.Index(existingSessionNames, sessionName)
	} else {
		selectedSessionIdx = c.MultiChoice(existingSessionNames, "Select a session:")
	}

	var session *autogen_client.Session
	if selectedSessionIdx == 0 {
		c.ShowPrompt(false)
		c.Print("Enter a session name: ")
		sessionName, err := c.ReadLineErr()
		if err != nil {
			c.Printf("Failed to read session name: %v\n", err)
			c.ShowPrompt(true)
			return
		}
		c.ShowPrompt(true)
		session, err = client.CreateSession(&autogen_client.CreateSession{
			UserID: cfg.UserID,
			Name:   sessionName,
			TeamID: team.Id,
		})
		if err != nil {
			c.Printf("Failed to create session: %v\n", err)
			return
		}
	} else {
		session = existingSessions[selectedSessionIdx-1]
	}

	promptStr := config.BoldGreen(fmt.Sprintf("%s--%s> ", team.Component.Label, session.Name))
	c.SetPrompt(promptStr)
	c.ShowPrompt(true)

	for {
		task, err := c.ReadLineErr()
		if err != nil {
			if errors.Is(err, readline.ErrInterrupt) {
				c.Println("exiting chat session...")
				return
			}
			c.Printf("Failed to read task: %v\n", err)
			return
		}
		if task == "exit" {
			c.Println("exiting chat session...")
			return
		}
		if task == "help" {
			c.Println("Available commands:")
			c.Println("  exit - exit the chat session")
			c.Println("  help - show this help message")
			continue
		}
		// Tool call requests and executions are sent as separate messages, but we should print them together
		// so if we receive a tool call request, we buffer it until we receive the corresponding tool call execution
		// We only need to buffer one request and one execution at a time
		var bufferedToolCallRequest *ToolCallRequestEvent
		// This is a map of agent source to whether we are currently streaming from that agent
		// If we are then we don't want to print the whole TextMessage, but only the content of the ModelStreamingEvent
		streaming := map[string]bool{}

		usage := &autogen_client.ModelsUsage{}

		// title := getThinkingVerb()
		// s := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
		// s.Suffix = " " + title
		// s.Start()
		// defer s.Stop()

		ch, err := client.InvokeSessionStream(session.ID, cfg.UserID, task)
		if err != nil {
			c.Printf("Failed to invoke session: %v\n", err)
			return
		}

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
					c.Println()
					continue
				}
				// Do not re-print the user's input, or system message asking for input
				if typed.Source == "user" || typed.Source == "system" {
					continue
				}
				c.Printf("%s: %s\n", config.BoldYellow("Event Type"), "TextMessage")
				c.Printf("%s: %s\n", config.BoldGreen("Source"), typed.Source)
				c.Println()
				c.Println(typed.Content)
				c.Println("----------------------------------")
				c.Println()
			case *ModelClientStreamingChunkEvent:
				usage.Add(typed.ModelsUsage)
				streaming[typed.Source] = true
				c.Printf(typed.Content)
			case *ToolCallRequestEvent:
				bufferedToolCallRequest = typed
			case *ToolCallExecutionEvent:
				if bufferedToolCallRequest == nil {
					c.Printf("Received tool call execution before request: %v\n", typed)
					continue
				}
				usage.Add(typed.ModelsUsage)
				c.Printf("%s: %s\n", config.BoldYellow("Event Type"), "ToolCall(s)")
				c.Printf("%s: %s\n", config.BoldGreen("Source"), typed.Source)
				if verbose {
					// For each function execution, find the corresponding tool call request and print them together
					for i, functionExecution := range typed.Content {
						for _, functionRequest := range bufferedToolCallRequest.Content {
							if functionExecution.CallID == functionRequest.ID {
								c.Println()
								c.Println("++++++++")
								c.Printf("Tool Call %d: (id: %s)\n", i, functionRequest.ID)
								c.Println()
								c.Printf("%s(%s)\n", functionRequest.Name, functionRequest.Arguments)
								c.Println()
								c.Println(functionExecution.Content)
								c.Println("++++++++")
								c.Println()
							}
						}
					}
				} else {
					tw := table.NewWriter()
					tw.AppendHeader(table.Row{"#", "Name", "Arguments"})
					for idx, functionRequest := range bufferedToolCallRequest.Content {
						tw.AppendRow(table.Row{idx, functionRequest.Name, functionRequest.Arguments})
					}
					c.Println(tw.Render())
				}

				c.Println("----------------------------------")
				c.Println()
				bufferedToolCallRequest = nil
			}
		}
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

const (
	TextMessageLabel                    = "TextMessage"
	ToolCallRequestEventLabel           = "ToolCallRequestEvent"
	ToolCallExecutionEventLabel         = "ToolCallExecutionEvent"
	StopMessageLabel                    = "StopMessage"
	HandoffMessageLabel                 = "HandoffMessage"
	ModelClientStreamingChunkEventLabel = "ModelClientStreamingChunkEvent"
	LLMCallEventMessageLabel            = "LLMCallEventMessage"
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
	default:
		return nil, fmt.Errorf("unknown event type: %s", baseEvent.Type)
	}
}

// Yes, this is AI generated, and so is this comment.
var thinkingVerbs = []string{"thinking", "processing", "mulling over", "pondering", "reflecting", "evaluating", "analyzing", "synthesizing", "interpreting", "inferring", "deducing", "reasoning", "evaluating", "synthesizing", "interpreting", "inferring", "deducing", "reasoning"}

func getThinkingVerb() string {
	return thinkingVerbs[rand.Intn(len(thinkingVerbs))]
}
