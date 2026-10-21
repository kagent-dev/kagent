package a2a

import (
	"context"
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"google.golang.org/adk/v2/server/adka2a/v2"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// isErrorMessageMetadataKey marks a status message as carrying an error. It is
// the key ADK sets on the failures it reports itself, so a client renders this
// one the same way.
var isErrorMessageMetadataKey = adka2a.ToA2AMetaKey("is_error_message")

// outputLimitWatch remembers how the turn's last model response ended.
//
// A model that stops at its output limit returns an answer cut mid-sentence,
// and the task must not report that as completed: an A2A client — a person's,
// or a parent agent that calls this one through the remote agent tool — has no
// other way to tell a whole answer from a truncated one. The finish reason is
// the provider-neutral signal; every model adapter maps its own stop reason
// onto it.
type outputLimitWatch struct {
	reason genai.FinishReason
	// generated is the output the response was billed for. When the model
	// stopped at the limit, that is the limit. Every adapter in pkg/models
	// reports the total billed output in CandidatesTokenCount, reasoning
	// included; ThoughtsTokenCount, where an adapter sets it, is a breakdown of
	// that total and must not be added to it.
	generated int32
}

type outputLimitWatchKey struct{}

// withOutputLimitWatch gives one execution its own watch. The callbacks below
// are configured once per executor and shared by every concurrent execution,
// so the state they read and write has to travel in the request's context.
func withOutputLimitWatch(ctx context.Context) context.Context {
	return context.WithValue(ctx, outputLimitWatchKey{}, &outputLimitWatch{})
}

func outputLimitWatchFrom(ctx context.Context) *outputLimitWatch {
	watch, _ := ctx.Value(outputLimitWatchKey{}).(*outputLimitWatch)
	return watch
}

// observe records how a model response ended. Streaming chunks and the events
// tools produce carry no finish reason, so only a completed model response
// moves the watch, and the last one of the turn is the one that decides.
func (w *outputLimitWatch) observe(event *adksession.Event) {
	if w == nil || event == nil || event.Partial {
		return
	}
	if event.FinishReason == "" || event.FinishReason == genai.FinishReasonUnspecified {
		return
	}
	w.reason = event.FinishReason
	w.generated = 0
	if usage := event.UsageMetadata; usage != nil {
		w.generated = usage.CandidatesTokenCount
	}
}

// failTruncatedAnswer turns the turn's completed terminal status into a failure
// when the last model response stopped at the output limit. The artifacts
// already streamed are left untouched, so the caller keeps the partial answer
// and reads the failure beside it.
//
// A status that is not completed is left alone: ADK is already reporting a
// failure of its own — a broken function call the truncation caused, say — or a
// pause waiting for input, and overwriting it would replace the cause with a
// symptom.
func (w *outputLimitWatch) failTruncatedAnswer(final *a2atype.TaskStatusUpdateEvent) {
	if w == nil || final == nil || w.reason != genai.FinishReasonMaxTokens {
		return
	}
	if final.Status.State != a2atype.TaskStateCompleted {
		return
	}
	part := a2atype.NewTextPart(outputLimitMessage(w.generated))
	part.Metadata = map[string]any{isErrorMessageMetadataKey: true}
	final.Status.State = a2atype.TaskStateFailed
	final.Status.Message = a2atype.NewMessageForTask(a2atype.MessageRoleAgent, final, part)
}

// outputLimitMessage names the limit when it is knowable. The response does not
// carry the request's max_tokens, but a model that stopped at the limit
// generated exactly that many tokens, so usage names it; without usage the
// number is left out rather than guessed.
func outputLimitMessage(generated int32) string {
	if generated <= 0 {
		return "model output limit reached: the answer is incomplete"
	}
	return fmt.Sprintf("model output limit reached (max_tokens=%d): the answer is incomplete", generated)
}
