package agent

import (
	"github.com/go-logr/logr"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// missingToolResult stands in for a result that was never recorded. It states
// no cause: the call may have been interrupted, or the process may have died
// between the two events. It matches the placeholder the Anthropic and OpenAI
// converters already use, so a request that reaches one of those unchanged
// behaves exactly as before.
const missingToolResult = "No response available for this function call."

// pendingToolResult stands in for a call ADK is deliberately holding open: a
// long-running tool, a human approval, or ask_user. No function response exists
// for these until the answer arrives, so the call is pending rather than lost.
// The distinction matters: told a tool "returned nothing", a model reissues it
// or proceeds; told the call is still awaiting a response, it can wait.
const pendingToolResult = "This call is awaiting a response and has not completed yet."

// MakeToolPairingCallback repairs tool call/response pairing in the model
// request.
//
// A tool call and its result are persisted as two separate session events. If a
// turn ends between them (process restart, OOM, client disconnect,
// cancellation), the session keeps a function call with no matching response.
// History is replayed on every later turn, and providers that require strict
// pairing reject the whole conversation, leaving the session unusable until it
// is deleted.
//
// The repair runs against the request rather than the store, so recorded
// history stays intact and sessions already broken heal on their next turn. It
// also sits above the session service, so it applies whichever store backs the
// session.
//
// ADK hands each request a deep copy of the content, so assigning Parts here
// cannot reach persisted history.
//
// Pairing is checked positionally, against the immediately following content,
// because that is the invariant the provider enforces.
//
// A conversation whose calls are all answered is left untouched, and any
// conversation this does change is one the provider would have rejected.
func MakeToolPairingCallback(log logr.Logger) llmagent.BeforeModelCallback {
	return func(ctx agent.Context, req *adkmodel.LLMRequest) (*adkmodel.LLMResponse, error) {
		if len(req.Contents) == 0 {
			return nil, nil
		}
		var source sessionSource
		if ctx != nil {
			source = ctx
		}
		repaired, synthesized := synthesizeMissingResponses(req.Contents, pendingCallIDs(source))
		if synthesized > 0 {
			// Logged because a repaired request means a turn ended between a
			// tool call and its result somewhere upstream.
			log.Info("Paired unanswered tool calls before the model request", "count", synthesized)
		}
		req.Contents = repaired
		return nil, nil
	}
}

// sessionSource is the part of agent.Context this file needs.
type sessionSource interface {
	Session() session.Session
}

// pendingCallIDs returns the ids of calls ADK is holding open for a
// long-running tool or an approval. They are read from the session events
// because LongRunningToolIDs lives on the event and does not survive the
// conversion to contents.
func pendingCallIDs(source sessionSource) map[string]bool {
	pending := map[string]bool{}
	if source == nil {
		return pending
	}
	current := source.Session()
	if current == nil {
		return pending
	}
	events := current.Events()
	if events == nil {
		return pending
	}
	for event := range events.All() {
		if event == nil {
			continue
		}
		for _, id := range event.LongRunningToolIDs {
			pending[id] = true
		}
	}
	return pending
}

// synthesizeMissingResponses gives every function call a function response in
// the immediately following content, and reports how many it had to supply.
func synthesizeMissingResponses(contents []*genai.Content, pending map[string]bool) ([]*genai.Content, int) {
	synthesized := 0
	repaired := make([]*genai.Content, 0, len(contents))
	for index, content := range contents {
		if content == nil {
			continue
		}
		repaired = append(repaired, content)

		calls := functionCalls(content)
		if len(calls) == 0 {
			continue
		}

		var following *genai.Content
		if index+1 < len(contents) {
			following = contents[index+1]
		}
		answered := responseIDs(following)
		missing := unanswered(calls, answered)
		if len(missing) == 0 {
			continue
		}

		synthesized += len(missing)
		parts := make([]*genai.Part, 0, len(missing))
		for _, call := range missing {
			parts = append(parts, &genai.Part{
				FunctionResponse: &genai.FunctionResponse{
					ID:       call.ID,
					Name:     call.Name,
					Response: map[string]any{"result": placeholderFor(call, pending)},
				},
			})
		}

		// Join an existing response turn so the results stay in one message;
		// otherwise the results need a turn of their own, before whatever
		// currently follows.
		if following != nil && len(answered) > 0 {
			following.Parts = append(following.Parts, parts...)
			continue
		}
		repaired = append(repaired, &genai.Content{Role: genai.RoleUser, Parts: parts})
	}
	return repaired, synthesized
}

func placeholderFor(call *genai.FunctionCall, pending map[string]bool) string {
	if call.ID != "" && pending[call.ID] {
		return pendingToolResult
	}
	return missingToolResult
}

// unanswered returns the calls with no response in answered. Ids are consumed
// one at a time rather than matched through a set, so a turn carrying several
// calls with no id (Gemini omits them, and ADK strips its own "adk-" ids before
// the request is built) does not have one response silently answer all of them.
func unanswered(calls []*genai.FunctionCall, answered []string) []*genai.FunctionCall {
	remaining := make([]string, len(answered))
	copy(remaining, answered)

	var missing []*genai.FunctionCall
	for _, call := range calls {
		matched := false
		for i, id := range remaining {
			if id == call.ID {
				remaining = append(remaining[:i], remaining[i+1:]...)
				matched = true
				break
			}
		}
		if !matched {
			missing = append(missing, call)
		}
	}
	return missing
}

func functionCalls(content *genai.Content) []*genai.FunctionCall {
	if content == nil {
		return nil
	}
	var calls []*genai.FunctionCall
	for _, part := range content.Parts {
		if part != nil && part.FunctionCall != nil {
			calls = append(calls, part.FunctionCall)
		}
	}
	return calls
}

func responseIDs(content *genai.Content) []string {
	if content == nil {
		return nil
	}
	var ids []string
	for _, part := range content.Parts {
		if part != nil && part.FunctionResponse != nil {
			ids = append(ids, part.FunctionResponse.ID)
		}
	}
	return ids
}
