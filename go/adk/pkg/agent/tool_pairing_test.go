package agent

import (
	"iter"
	"testing"
	"time"

	"github.com/go-logr/logr"

	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
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
	return &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{
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

// fakeEvents / fakeSession / fakeContext stand in for the session an
// agent.Context exposes. Only Session() is consulted by the code under test.
type fakeEvents []*session.Event

func (e fakeEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for _, event := range e {
			if !yield(event) {
				return
			}
		}
	}
}
func (e fakeEvents) Len() int                { return len(e) }
func (e fakeEvents) At(i int) *session.Event { return e[i] }

type fakeSession struct{ events fakeEvents }

func (s fakeSession) ID() string                { return "s" }
func (s fakeSession) AppName() string           { return "app" }
func (s fakeSession) UserID() string            { return "u" }
func (s fakeSession) State() session.State      { return nil }
func (s fakeSession) Events() session.Events    { return s.events }
func (s fakeSession) LastUpdateTime() time.Time { return time.Time{} }

type fakeContext struct{ session session.Session }

func (c fakeContext) Session() session.Session { return c.session }

// pendingEvent is an event whose tool calls ADK is holding open (approval,
// ask_user, long-running tool).
func pendingEvent(ids ...string) *session.Event {
	return &session.Event{LongRunningToolIDs: ids}
}

func withPending(ids ...string) sessionSource {
	return fakeContext{session: fakeSession{events: fakeEvents{pendingEvent(ids...)}}}
}

func repair(contents []*genai.Content) []*genai.Content {
	return repairWith(contents, nil)
}

func repairWith(contents []*genai.Content, source sessionSource) []*genai.Content {
	repaired, _ := synthesizeMissingResponses(contents, pendingCallIDs(source))
	return repaired
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

func TestToolPairingCallbackWiring(t *testing.T) {
	t.Parallel()

	req := &adkmodel.LLMRequest{Contents: []*genai.Content{callContent("abc")}}
	if _, err := MakeToolPairingCallback(logr.Discard())(nil, req); err != nil {
		t.Fatalf("callback returned error: %v", err)
	}
	if len(req.Contents) != 2 {
		t.Fatalf("expected the callback to answer the call, got %d contents", len(req.Contents))
	}
}

func TestToolPairingSynthesizesResultForDanglingCall(t *testing.T) {
	t.Parallel()

	contents := repair([]*genai.Content{textContent(genai.RoleUser, "what's failing?"), callContent("abc")})

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

	contents := repair([]*genai.Content{callContent("abc"), textContent(genai.RoleUser, "second message")})

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
		textContent(genai.RoleUser, "are you there?"),
		responseContent("abc", "late result"),
	})

	responses := responsesIn(contents[1])
	if len(responses) != 1 || responses[0].Response["result"] != missingToolResult {
		t.Fatalf("expected a placeholder adjacent to the call, got %+v", responses)
	}
}

// TestToolPairingIDLessSiblingsEachGetAResponse guards the id-matching rule:
// Gemini omits call ids and ADK strips its own "adk-" ids before the request is
// built, so one response must not answer every id-less call in the turn.
func TestToolPairingIDLessSiblingsEachGetAResponse(t *testing.T) {
	t.Parallel()

	callTurn := &genai.Content{Role: "model", Parts: []*genai.Part{
		{FunctionCall: &genai.FunctionCall{Name: "get_pods"}},
		{FunctionCall: &genai.FunctionCall{Name: "get_logs"}},
	}}
	answered := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{
		{FunctionResponse: &genai.FunctionResponse{Name: "get_pods", Response: map[string]any{"result": "ok"}}},
	}}

	contents := repair([]*genai.Content{callTurn, answered})

	responses := responsesIn(contents[1])
	if len(responses) != 2 {
		t.Fatalf("expected both id-less calls to be answered, got %d responses", len(responses))
	}
	if responses[0].Response["result"] != "ok" {
		t.Errorf("existing result was overwritten: %+v", responses[0])
	}
	if responses[1].Response["result"] != missingToolResult {
		t.Errorf("expected a placeholder for the second call, got %+v", responses[1])
	}
}

// TestToolPairingPendingApprovalKeepsPendingWording covers a call ADK is holding
// open. A plain message sent while an approval is pending reaches this callback
// as an ordinary turn, and reporting an empty result would invite the model to
// reissue the approval or proceed without it.
func TestToolPairingPendingApprovalKeepsPendingWording(t *testing.T) {
	t.Parallel()

	contents := repairWith(
		[]*genai.Content{callContent("call-1"), textContent(genai.RoleUser, "never mind, what about X?")},
		withPending("call-1"),
	)

	responses := responsesIn(contents[1])
	if len(responses) != 1 || responses[0].Response["result"] != pendingToolResult {
		t.Fatalf("expected the pending placeholder, got %+v", responses)
	}
}

func TestToolPairingInterruptedCallKeepsMissingWording(t *testing.T) {
	t.Parallel()

	contents := repairWith(
		[]*genai.Content{callContent("other"), textContent(genai.RoleUser, "still there?")},
		withPending("call-1"),
	)

	responses := responsesIn(contents[1])
	if len(responses) != 1 || responses[0].Response["result"] != missingToolResult {
		t.Fatalf("expected the neutral placeholder, got %+v", responses)
	}
}

func TestToolPairingLeavesHealthyHistoryUntouched(t *testing.T) {
	t.Parallel()

	original := []*genai.Content{
		textContent(genai.RoleUser, "what's failing?"),
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

func TestToolPairingLeavesAnsweredPendingCallUntouched(t *testing.T) {
	t.Parallel()

	original := []*genai.Content{callContent("call-1"), responseContent("call-1", "approved")}

	contents := repairWith(original, withPending("call-1"))

	if len(contents) != 2 {
		t.Fatalf("expected the answered pending call to be left alone, got %d contents", len(contents))
	}
	if responsesIn(contents[1])[0].Response["result"] != "approved" {
		t.Errorf("real result was replaced: %+v", responsesIn(contents[1])[0])
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

// assertPaired walks the contents independently of the production helpers and
// asserts the invariant the Anthropic and Bedrock APIs enforce: every tool call
// must be answered in the immediately following content.
func assertPaired(t *testing.T, contents []*genai.Content) {
	t.Helper()
	for i, content := range contents {
		var ids []string
		for _, part := range content.Parts {
			if part != nil && part.FunctionCall != nil {
				ids = append(ids, part.FunctionCall.ID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		if i+1 >= len(contents) {
			t.Errorf("content %d ends the conversation with an unanswered call", i)
			continue
		}
		answered := map[string]int{}
		for _, part := range contents[i+1].Parts {
			if part != nil && part.FunctionResponse != nil {
				answered[part.FunctionResponse.ID]++
			}
		}
		for _, id := range ids {
			if answered[id] == 0 {
				t.Errorf("call %q has no result in the following content", id)
				continue
			}
			answered[id]--
		}
	}
}

func TestToolPairingSatisfiesProviderAdjacency(t *testing.T) {
	t.Parallel()

	cases := map[string][]*genai.Content{
		"interrupted turn":            {textContent(genai.RoleUser, "what's failing?"), callContent("abc")},
		"message during running tool": {callContent("abc"), textContent(genai.RoleUser, "second message")},
		"partial answer":              {callContent("abc", "def"), responseContent("abc", "pod X running")},
	}

	for name, contents := range cases {
		t.Run(name, func(t *testing.T) {
			assertPaired(t, repair(contents))
		})
	}
}

// TestUnrepairedHistoryFailsAdjacency guards the assertion above: it must
// reject the broken input, otherwise it would pass on everything.
func TestUnrepairedHistoryFailsAdjacency(t *testing.T) {
	t.Parallel()

	fake := &testing.T{}
	assertPaired(fake, []*genai.Content{textContent(genai.RoleUser, "what's failing?"), callContent("abc")})
	if !fake.Failed() {
		t.Error("adjacency assertion accepted an unrepaired conversation")
	}
}
