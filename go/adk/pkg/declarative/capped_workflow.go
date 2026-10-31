package declarative

import (
	"encoding/json"
	"fmt"
	"iter"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"
)

// cappedWorkflowAgent builds a Workflow agent whose graph runs at most
// maxConcurrency nodes at once.
//
// workflowagent.Config does not accept workflow.WithMaxConcurrency, so this
// mirrors workflowagent.New: a fresh turn runs the graph and a turn that
// answers a hitl request resumes it. The agent is not tagged as a workflow
// agent for telemetry, so its span is named invoke_agent.
func cappedWorkflowAgent(name, description string, subs []agent.Agent, edges []workflow.Edge, maxConcurrency int) (agent.Agent, error) {
	w, err := workflow.New(name, edges, workflow.WithMaxConcurrency(maxConcurrency))
	if err != nil {
		return nil, err
	}
	return agent.New(agent.Config{
		Name:        name,
		Description: description,
		SubAgents:   subs,
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return runOrResume(ctx, w)
		},
	})
}

func runOrResume(ctx agent.InvocationContext, w *workflow.Workflow) iter.Seq2[*session.Event, error] {
	responses := inputResponses(ctx.UserContent())
	if len(responses) == 0 {
		return w.Run(ctx)
	}
	// The runner reuses the paused run's invocation ID on resume, which keeps
	// an earlier completed run of the session out of the rebuilt state.
	state, err := w.ReconstructRunState(ctx.Session(), ctx.InvocationID())
	if err != nil {
		return func(yield func(*session.Event, error) bool) {
			yield(nil, fmt.Errorf("failed to rebuild workflow %q for resume: %w", w.Name(), err))
		}
	}
	if state == nil {
		return w.Run(ctx)
	}
	return w.Resume(agent.Promote(ctx), state, responses)
}

// inputResponses returns the answers to workflow input requests by interrupt
// ID, decoded the same way as workflowagent.
func inputResponses(c *genai.Content) map[string]any {
	if c == nil {
		return nil
	}
	responses := map[string]any{}
	for _, p := range c.Parts {
		if p == nil || p.FunctionResponse == nil || p.FunctionResponse.Name != workflow.WorkflowInputFunctionCallName {
			continue
		}
		responses[p.FunctionResponse.ID] = decodeInputResponse(p.FunctionResponse.Response)
	}
	return responses
}

func decodeInputResponse(response map[string]any) any {
	if raw, ok := response["response"]; ok {
		if s, isStr := raw.(string); isStr {
			var decoded any
			if json.Unmarshal([]byte(s), &decoded) == nil {
				return decoded
			}
			return s
		}
		return raw
	}
	if payload, ok := response["payload"]; ok {
		return payload
	}
	return response
}
