package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/abiosoft/ishell/v2"
	"github.com/abiosoft/readline"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	"github.com/spf13/pflag"
	"k8s.io/utils/ptr"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
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
	clientSet := config.GetClient(c)

	var agentResp *api.AgentResponse
	if len(flagSet.Args()) > 0 {
		agentName := flagSet.Args()[0]
		var err error
		agtResp, err := clientSet.Agent.GetAgent(context.Background(), agentName)
		if err != nil {
			c.Println(err)
			return
		}
		agentResp = agtResp.Data
	}
	// If agent is not found or not passed as an argument, prompt the user to select from available agents
	if agentResp == nil {
		c.Printf("Please select from available agents.\n")
		// Get the agents based on the input + userID
		agentListResp, err := clientSet.Agent.ListAgents(context.Background())
		if err != nil {
			c.Println(err)
			return
		}

		if len(agentListResp.Data) == 0 {
			c.Println("No agents found, please create one via the web UI or CRD before chatting.")
			return
		}

		agentNames := make([]string, len(agentListResp.Data))
		for i, agent := range agentListResp.Data {
			agentNames[i] = utils.ConvertToKubernetesIdentifier(agent.ID)
		}

		selectedAgentIdx := c.MultiChoice(agentNames, "Select an agent:")
		agentResp = &agentListResp.Data[selectedAgentIdx]
	}

	sessions, err := clientSet.Session.ListSessions(context.Background())
	if err != nil {
		c.Println(err)
		return
	}

	existingSessions := slices.Collect(utils.Filter(slices.Values(sessions.Data), func(session *api.Session) bool { return true }))

	// filter by selected agentRef
	existingSessions = slices.Collect(utils.Filter(slices.Values(existingSessions), func(session *api.Session) bool {
		return session.AgentID != nil && *session.AgentID == agentResp.ID
	}))

	existingSessionNames := slices.Collect(utils.Map(slices.Values(existingSessions), func(session *api.Session) string {
		if session.Name != nil {
			return fmt.Sprintf("%s (ID: %s)", *session.Name, session.ID)
		}
		return session.ID
	}))

	// Add the new session option to the beginning of the list
	existingSessionNames = append([]string{sessionCreateNew}, existingSessionNames...)
	var selectedSessionIdx int
	if sessionName != "" {
		selectedSessionIdx = slices.Index(existingSessionNames, sessionName)
	} else {
		selectedSessionIdx = c.MultiChoice(existingSessionNames, "Select a session:")
	}

	agentRef := utils.ConvertToKubernetesIdentifier(agentResp.ID)

	var session *api.Session
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
		sessionResp, err := clientSet.Session.CreateSession(context.Background(), &api.SessionRequest{
			Name:     ptr.To(sessionName),
			AgentRef: ptr.To(agentRef),
		})
		if err != nil {
			c.Printf("Failed to create session: %v\n", err)
			return
		}
		session = sessionResp.Data
	} else {
		session = existingSessions[selectedSessionIdx-1]
		// Adding this print as a sort of a hacky solution to an issue where the prompt is not shown after the session is chosen unless we print something.
		c.Print("")
	}

	// Setup A2A client
	a2aURL := fmt.Sprintf("%s/api/a2a/%s", cfg.KAgentURL, agentRef)
	a2aClient, err := a2aclient.NewA2AClient(a2aURL)
	if err != nil {
		c.Printf("Failed to create A2A client: %v\n", err)
		return
	}

	// Port forwarding is already handled by the interactive mode - no need to start another one

	var sessionNameStr string
	if session.Name != nil {
		sessionNameStr = *session.Name
	} else {
		sessionNameStr = session.ID
	}
	promptStr := config.BoldGreen(fmt.Sprintf("%s (%s)> ", agentRef, sessionNameStr))
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

		// Use A2A client to send message
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)

		sessionID := session.ID
		result, err := a2aClient.StreamMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Role:      protocol.MessageRoleUser,
				ContextID: &sessionID,
				Parts:     []protocol.Part{protocol.NewTextPart(task)},
			},
		})
		if err != nil {
			c.Printf("Failed to invoke session: %v\n", err)
			cancel()
			continue
		}

		StreamA2AEvents(result, verbose)
		cancel()
	}
}
