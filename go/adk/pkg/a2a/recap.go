package a2a

import (
	"context"
	"slices"
	"strings"
	"time"

	adksession "google.golang.org/adk/v2/session"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

const (
	// recapMinTurns is the number of user turns a chat needs before it gets a recap.
	recapMinTurns = 3
	// recapTranscriptMaxLength caps the transcript, in characters, sent to the model.
	recapTranscriptMaxLength = 20000
	// recapMaxLength is the longest recap, in characters, the session API accepts.
	recapMaxLength = 2000
	// recapCleanupWait bounds how long a turn stays open after the answer
	// while the recap is written.
	recapCleanupWait = 8 * time.Second
	// recapGenerationTimeout bounds the model call that writes a recap.
	recapGenerationTimeout = 60 * time.Second
)

// Recapper writes a short recap of a chat from its transcript.
type Recapper interface {
	Recap(ctx context.Context, transcript string) (string, error)
}

// eventText joins the text parts of an event, leaving out thoughts.
func eventText(event *adksession.Event) string {
	if event == nil || event.Content == nil {
		return ""
	}
	var texts []string
	for _, part := range event.Content.Parts {
		if part != nil && part.Text != "" && !part.Thought {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// countUserTurns counts the events in which the user sent text.
func countUserTurns(events []*adksession.Event) int {
	turns := 0
	for _, event := range events {
		if event != nil && event.Author == "user" && eventText(event) != "" {
			turns++
		}
	}
	return turns
}

// buildTranscript labels the user and agent text of the events, keeping the
// most recent maxLength characters.
func buildTranscript(events []*adksession.Event, maxLength int) string {
	var lines []string
	for _, event := range events {
		text := eventText(event)
		if text == "" {
			continue
		}
		label := "Agent"
		if event.Author == "user" {
			label = "User"
		}
		lines = append(lines, label+": "+text)
	}
	runes := []rune(strings.Join(lines, "\n"))
	if len(runes) > maxLength {
		runes = runes[len(runes)-maxLength:]
	}
	return string(runes)
}

// pendingRecap is a recap being generated in the background.
type pendingRecap struct {
	sessionID string
	turns     int
	cancel    context.CancelFunc
	// done closes after text is set.
	done chan struct{}
	text string
}

// startRecap begins a recap when the chat has enough turns.
func (e *KAgentExecutor) startRecap(ctx context.Context, userID, sessionID string) {
	if e.recapper == nil || e.controllerClient == nil {
		return
	}
	resp, err := e.sessionService.Get(ctx, &adksession.GetRequest{AppName: e.appName, UserID: userID, SessionID: sessionID})
	if err != nil || resp == nil || resp.Session == nil {
		e.logger.DebugContext(ctx, "recap skipped: session unavailable", "error", err, "session_id", sessionID)
		return
	}
	events := slices.Collect(resp.Session.Events().All())
	turns := countUserTurns(events)
	if turns < recapMinTurns {
		return
	}
	transcript := buildTranscript(events, recapTranscriptMaxLength)

	// The call outlives the request but not its own deadline or the next request.
	recapCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recapGenerationTimeout)
	recap := &pendingRecap{sessionID: sessionID, turns: turns, cancel: cancel, done: make(chan struct{})}
	e.cancelRecap()
	e.recapMu.Lock()
	e.recap = recap
	e.recapMu.Unlock()
	go func() {
		defer close(recap.done)
		defer cancel()
		text, err := e.recapper.Recap(recapCtx, transcript)
		if err != nil {
			e.logger.DebugContext(ctx, "recap generation failed", "error", err, "session_id", sessionID)
			return
		}
		recap.text = truncateRunes(strings.TrimSpace(text), recapMaxLength)
	}()
}

// truncateRunes cuts text to at most max characters.
func truncateRunes(text string, max int) string {
	if runes := []rune(text); len(runes) > max {
		return string(runes[:max])
	}
	return text
}

// cancelRecap abandons any in-flight recap.
func (e *KAgentExecutor) cancelRecap() {
	e.recapMu.Lock()
	recap := e.recap
	e.recap = nil
	e.recapMu.Unlock()
	if recap != nil {
		recap.cancel()
		<-recap.done
	}
}

// waitRecap waits for the in-flight recap, then stores it.
func (e *KAgentExecutor) waitRecap(ctx context.Context, sessionID string) {
	e.recapMu.Lock()
	recap := e.recap
	if recap != nil && recap.sessionID == sessionID {
		e.recap = nil
	} else {
		recap = nil
	}
	e.recapMu.Unlock()
	if recap == nil {
		return
	}
	timer := time.NewTimer(e.recapWait)
	defer timer.Stop()
	select {
	case <-recap.done:
	case <-timer.C:
		e.logger.DebugContext(ctx, "recap not ready in time", "session_id", sessionID)
	case <-ctx.Done():
	}
	recap.cancel()
	<-recap.done
	if recap.text == "" || ctx.Err() != nil {
		return
	}
	callCtx, callCancel := e.controllerClient.CallContext(ctx, "")
	defer callCancel()
	if _, err := e.controllerClient.SessionService().SetSessionRecap(callCtx, &apiv1.SetSessionRecapRequest{
		SessionId: sessionID, Text: recap.text, TurnCount: int32(recap.turns),
	}); err != nil {
		e.logger.WarnContext(ctx, "failed to set session recap", "error", err, "session_id", sessionID)
	}
}
