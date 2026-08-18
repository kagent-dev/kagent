package agent

import (
	"testing"

	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func callContent(ids ...string) *genai.Content {
	parts := make([]*genai.Part, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: id, Name: "get_pods"}})
	}
	return &genai.Content{Role: "model", Parts: parts}
}

func responseContent(id string, result string) *genai.Content {
	return &genai.Content{Role: "user", Parts: []*genai.Part{{
		FunctionResponse: &genai.FunctionResponse{
			ID:       id,
			Name:     "get_pods",
			Response: map[string]any{"result": result},
		},
	}}}
}

func textContent(role, text string) *genai.Content {
	return &genai.Content{Role: role, Parts: []*genai.Part{{Text: text}}}
}

func repair(contents []*genai.Content) []*genai.Content {
	req := &adkmodel.LLMRequest{Contents: contents}
	if _, err := MakeToolPairingCallback()(nil, req); err != nil {
		panic(err)
	}
	return req.Contents
}

func responsesIn(content *genai.Content) []*genai.FunctionResponse {
	var responses []*genai.FunctionResponse
	for _, part := range content.Parts {
		if part.FunctionResponse != nil {
			responses = append(responses, part.FunctionResponse)
		}
	}
	return responses
}

func TestToolPairingSynthesizesResultForDanglingCall(t *testing.T) {
	t.Parallel()

	contents := repair([]*genai.Content{textContent("user", "what's failing?"), callContent("abc")})

	if len(contents) != 3 {
		t.Fatalf("expected a synthesized response turn, got %d contents", len(contents))
	}
	responses := responsesIn(contents[2])
	if len(responses) != 1 || responses[0].ID != "abc" {
		t.Fatalf("expected one response for abc, got %+v", responses)
	}
	if got := responses[0].Response["result"]; got != missingToolResult {
		t.Errorf("result = %v, want %q", got, missingToolResult)
	}
}

func TestToolPairingInsertsResultBeforeFollowingUserMessage(t *testing.T) {
	t.Parallel()

	contents := repair([]*genai.Content{callContent("abc"), textContent("user", "second message")})

	if len(contents) != 3 {
		t.Fatalf("expected 3 contents, got %d", len(contents))
	}
	if responses := responsesIn(contents[1]); len(responses) != 1 || responses[0].ID != "abc" {
		t.Fatalf("expected the result between call and message, got %+v", responses)
	}
	if contents[2].Parts[0].Text != "second message" {
		t.Errorf("user message was not preserved after the synthesized result")
	}
}

func TestToolPairingReusesSiblingResponseTurn(t *testing.T) {
	t.Parallel()

	contents := repair([]*genai.Content{callContent("abc", "def"), responseContent("abc", "pod X running")})

	if len(contents) != 2 {
		t.Fatalf("expected the existing response turn to be reused, got %d contents", len(contents))
	}
	responses := responsesIn(contents[1])
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}
	if responses[0].Response["result"] != "pod X running" {
		t.Errorf("existing result was overwritten: %+v", responses[0])
	}
	if responses[1].ID != "def" || responses[1].Response["result"] != missingToolResult {
		t.Errorf("expected a placeholder for def, got %+v", responses[1])
	}
}

func TestToolPairingRepairsNonAdjacentResponse(t *testing.T) {
	t.Parallel()

	contents := repair([]*genai.Content{
		callContent("abc"),
		textContent("user", "are you there?"),
		responseContent("abc", "late result"),
	})

	responses := responsesIn(contents[1])
	if len(responses) != 1 || responses[0].Response["result"] != missingToolResult {
		t.Fatalf("expected a placeholder adjacent to the call, got %+v", responses)
	}
}

func TestToolPairingDropsOrphanedResponse(t *testing.T) {
	t.Parallel()

	contents := repair([]*genai.Content{textContent("user", "hello"), responseContent("abc", "stale")})

	if len(contents) != 1 {
		t.Fatalf("expected the orphaned response turn to be dropped, got %d contents", len(contents))
	}
	if contents[0].Parts[0].Text != "hello" {
		t.Errorf("wrong content survived: %+v", contents[0])
	}
}

func TestToolPairingDropsOnlyTheOrphanedResponse(t *testing.T) {
	t.Parallel()

	responseTurn := &genai.Content{Role: "user", Parts: []*genai.Part{
		{FunctionResponse: &genai.FunctionResponse{ID: "abc", Name: "get_pods", Response: map[string]any{"result": "ok"}}},
		{FunctionResponse: &genai.FunctionResponse{ID: "zzz", Name: "ghost", Response: map[string]any{"result": "stale"}}},
	}}

	contents := repair([]*genai.Content{callContent("abc"), responseTurn})

	responses := responsesIn(contents[1])
	if len(responses) != 1 || responses[0].ID != "abc" {
		t.Fatalf("expected only the orphan to be dropped, got %+v", responses)
	}
}

func TestToolPairingLeavesHealthyHistoryUntouched(t *testing.T) {
	t.Parallel()

	original := []*genai.Content{
		textContent("user", "what's failing?"),
		callContent("abc"),
		responseContent("abc", "pod X running"),
		textContent("model", "pod X is down"),
	}

	contents := repair(original)

	if len(contents) != len(original) {
		t.Fatalf("healthy history was modified: %d contents, want %d", len(contents), len(original))
	}
	for i := range contents {
		if contents[i] != original[i] {
			t.Errorf("content %d was replaced", i)
		}
	}
}

func TestToolPairingHandlesEmptyInput(t *testing.T) {
	t.Parallel()

	if contents := repair(nil); len(contents) != 0 {
		t.Errorf("expected no contents, got %d", len(contents))
	}
	if contents := repair([]*genai.Content{nil, callContent("abc")}); len(contents) != 2 {
		t.Errorf("expected the nil content to be dropped and the call answered, got %d", len(contents))
	}
}

// TestToolPairingSatisfiesAnthropicAdjacency asserts the invariant the Anthropic
// API enforces: every tool_use must be answered in the immediately following
// message.
func TestToolPairingSatisfiesAnthropicAdjacency(t *testing.T) {
	t.Parallel()

	cases := map[string][]*genai.Content{
		"interrupted turn": {textContent("user", "what's failing?"), callContent("abc")},
		"double message":   {callContent("abc"), textContent("user", "second message")},
		"partial answer":   {callContent("abc", "def"), responseContent("abc", "pod X running")},
	}

	for name, contents := range cases {
		t.Run(name, func(t *testing.T) {
			repaired := repair(contents)
			for i, content := range repaired {
				calls := functionCalls(content)
				if len(calls) == 0 {
					continue
				}
				if i+1 >= len(repaired) {
					t.Fatalf("content %d ends the conversation with an unanswered call", i)
				}
				answered := responseIDs(repaired[i+1])
				for _, call := range calls {
					if !answered[call.ID] {
						t.Errorf("call %q has no result in the following content", call.ID)
					}
				}
			}
		})
	}
}
