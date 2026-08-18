package agent

import (
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// missingToolResult stands in for a tool result that was never recorded. It
// deliberately states no cause: the call may have been interrupted, or may
// belong to a long-running tool that has not returned yet. It matches the
// placeholder the provider converters already use, so a request that reaches
// one of those unchanged behaves exactly as before.
const missingToolResult = "No response available for this function call."

// MakeToolPairingCallback repairs tool call/response pairing in the model
// request.
//
// A tool call and its result are persisted as two separate session events. If a
// turn ends between them (process restart, OOM, client disconnect, cancellation,
// or a second message arriving while a slow tool is still running), the session
// keeps a function call with no matching response. History is replayed verbatim
// on every later turn, and providers that require strict pairing reject the
// whole conversation, leaving the session unusable until it is deleted.
//
// The repair runs against the request rather than the store, so recorded history
// stays intact for the UI and sessions already broken heal on their next turn.
// Pairing is checked positionally, against the immediately following content,
// because that is the invariant the provider enforces; a response elsewhere in
// the history does not satisfy it.
//
// A conversation whose calls are all answered is left untouched, and any
// conversation this does change is one the provider would have rejected.
func MakeToolPairingCallback() llmagent.BeforeModelCallback {
	return func(_ agent.Context, req *adkmodel.LLMRequest) (*adkmodel.LLMResponse, error) {
		if len(req.Contents) == 0 {
			return nil, nil
		}
		req.Contents = synthesizeMissingResponses(dropOrphanedResponses(req.Contents))
		return nil, nil
	}
}

// dropOrphanedResponses removes function responses whose call is not present in
// the immediately preceding content, and drops any content left empty.
func dropOrphanedResponses(contents []*genai.Content) []*genai.Content {
	kept := make([]*genai.Content, 0, len(contents))
	for index, content := range contents {
		if content == nil {
			continue
		}
		if !hasFunctionResponse(content) {
			kept = append(kept, content)
			continue
		}

		var answerable map[string]bool
		if index > 0 {
			answerable = callIDs(contents[index-1])
		}

		parts := make([]*genai.Part, 0, len(content.Parts))
		for _, part := range content.Parts {
			if part == nil {
				continue
			}
			if part.FunctionResponse != nil && !answerable[part.FunctionResponse.ID] {
				continue
			}
			parts = append(parts, part)
		}
		if len(parts) == 0 {
			continue
		}
		content.Parts = parts
		kept = append(kept, content)
	}
	return kept
}

// synthesizeMissingResponses gives every function call a function response in
// the immediately following content.
func synthesizeMissingResponses(contents []*genai.Content) []*genai.Content {
	repaired := make([]*genai.Content, 0, len(contents))
	for index, content := range contents {
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

		parts := make([]*genai.Part, 0, len(calls))
		for _, call := range calls {
			if answered[call.ID] {
				continue
			}
			parts = append(parts, &genai.Part{
				FunctionResponse: &genai.FunctionResponse{
					ID:       call.ID,
					Name:     call.Name,
					Response: map[string]any{"result": missingToolResult},
				},
			})
		}
		if len(parts) == 0 {
			continue
		}

		// Join an existing response turn so the results stay in one message;
		// otherwise the results need a turn of their own, before whatever
		// currently follows.
		if following != nil && len(answered) > 0 {
			following.Parts = append(following.Parts, parts...)
			continue
		}
		repaired = append(repaired, &genai.Content{Role: "user", Parts: parts})
	}
	return repaired
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

func callIDs(content *genai.Content) map[string]bool {
	ids := map[string]bool{}
	for _, call := range functionCalls(content) {
		ids[call.ID] = true
	}
	return ids
}

func responseIDs(content *genai.Content) map[string]bool {
	ids := map[string]bool{}
	if content == nil {
		return ids
	}
	for _, part := range content.Parts {
		if part != nil && part.FunctionResponse != nil {
			ids[part.FunctionResponse.ID] = true
		}
	}
	return ids
}

func hasFunctionResponse(content *genai.Content) bool {
	return len(responseIDs(content)) > 0
}
