package anthropicworker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

var errSessionEnded = errors.New("provider session ended")

func bearer(token string) []option.RequestOption {
	return []option.RequestOption{option.WithHeaderDel("X-Api-Key"), option.WithAuthToken(token)}
}

func sessionsToken(secret string) string {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(secret, "="))
	if err != nil {
		return ""
	}
	var payload struct {
		Token string `json:"sessions_token"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	return payload.Token
}

func providerError(operation string, err error) error {
	var upstream *anthropic.Error
	if errors.As(err, &upstream) {
		return fmt.Errorf("%s: Anthropic HTTP %d", operation, upstream.StatusCode)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return fmt.Errorf("%s: Anthropic request failed", operation)
}

func providerStatus(err error, status int) bool {
	var upstream *anthropic.Error
	return errors.As(err, &upstream) && upstream.StatusCode == status
}

func (r *Runner) poll(ctx context.Context, env Environment) {
	for ctx.Err() == nil {
		call, cancel := context.WithTimeout(ctx, apiTimeout)
		var raw *http.Response
		work, err := r.provider.Beta.Environments.Work.Poll(call, env.ID, anthropic.BetaEnvironmentWorkPollParams{
			BlockMs: anthropic.Int(999),
		}, append(bearer(env.Key), option.WithResponseInto(&raw))...)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if raw != nil && raw.StatusCode == http.StatusNoContent {
			wait(ctx, tickInterval)
			continue
		}
		if err != nil {
			r.warn(ctx, "poll assignment", providerError("poll", err))
			wait(ctx, retryDelay)
			continue
		}
		if work == nil || work.ID == "" || work.Data.ID == "" {
			wait(ctx, tickInterval)
			continue
		}
		token := sessionsToken(work.Secret)
		encrypted := r.cipher.Seal(nil, nil, []byte(token), []byte(work.ID))
		call, cancel = context.WithTimeout(ctx, dbTimeout)
		err = r.store.enqueue(call, assignment{id: work.ID, environment: env.ID, sessionID: work.Data.ID, credential: encrypted})
		cancel()
		if err != nil {
			r.warn(ctx, "persist assignment", err)
		}
		// Poll can return an unacknowledged assignment again immediately.
		wait(ctx, tickInterval)
	}
}

func (r *Runner) acknowledge(ctx context.Context, w assignment, env Environment) error {
	call, cancel := context.WithTimeout(context.WithoutCancel(ctx), apiTimeout)
	defer cancel()
	work := r.provider.Beta.Environments.Work
	_, err := work.Ack(call, w.id, anthropic.BetaEnvironmentWorkAckParams{EnvironmentID: w.environment}, bearer(env.Key)...)
	if err != nil {
		if providerStatus(err, 404) || providerStatus(err, 410) {
			return errSessionEnded
		}
		// A crash can land after the initial Ack but before the local commit.
		if !providerStatus(err, 409) {
			return providerError("acknowledge", err)
		}
		current, getErr := work.Get(call, w.id, anthropic.BetaEnvironmentWorkGetParams{EnvironmentID: w.environment}, bearer(env.Key)...)
		if getErr != nil || current.AcknowledgedAt == "" {
			return providerError("acknowledge", err)
		}
	}
	return r.store.ack(call, w)
}

func (r *Runner) heartbeat(ctx context.Context, w assignment, auth []option.RequestOption, ready chan<- struct{}) error {
	expected := w.heartbeat
	if expected == "" {
		expected = "NO_HEARTBEAT"
	}
	for first := true; ctx.Err() == nil; first = false {
		// Complete an in-flight heartbeat and save its continuation token on shutdown.
		call, cancel := context.WithTimeout(context.WithoutCancel(ctx), apiTimeout)
		response, err := r.provider.Beta.Environments.Work.Heartbeat(call, w.id, anthropic.BetaEnvironmentWorkHeartbeatParams{
			EnvironmentID: w.environment, ExpectedLastHeartbeat: anthropic.String(expected),
		}, auth...)
		cancel()
		if providerStatus(err, 404) || providerStatus(err, 410) ||
			(response != nil && response.State == anthropic.BetaSelfHostedWorkHeartbeatResponseStateStopped) {
			// A stopped Work item does not close the session SSE or invalidate
			// results. Leave that connection and its commands alone.
			if first {
				close(ready)
			}
			return nil
		}
		if err != nil {
			return providerError("heartbeat", err)
		}
		if response == nil || !response.LeaseExtended || response.LastHeartbeat == "" {
			return errors.New("provider did not extend the assignment")
		}
		// Persist the continuation token even when shutdown arrived during HTTP.
		call, cancel = context.WithTimeout(context.WithoutCancel(ctx), dbTimeout)
		err = r.store.heartbeat(call, w, response.LastHeartbeat)
		cancel()
		if err != nil {
			return err
		}
		expected = response.LastHeartbeat
		if first {
			close(ready)
		}
		wait(ctx, time.Duration(max(1, min(15, response.TTLSeconds/3)))*time.Second)
	}
	return ctx.Err()
}

func (r *Runner) sendResult(ctx context.Context, w assignment, c command, auth []option.RequestOption) error {
	call, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	event := anthropic.BetaManagedAgentsUserToolResultEventParams{
		ToolUseID: c.id, IsError: anthropic.Bool(c.isError),
		Type: anthropic.BetaManagedAgentsUserToolResultEventParamsTypeUserToolResult,
		Content: []anthropic.BetaManagedAgentsUserToolResultEventParamsContentUnion{{
			OfText: &anthropic.BetaManagedAgentsTextBlockParam{Text: c.result, Type: "text"},
		}},
	}
	_, err := r.provider.Beta.Sessions.Events.Send(call, w.sessionID, anthropic.BetaSessionEventSendParams{
		Events: []anthropic.BetaManagedAgentsEventParamsUnion{{OfUserToolResult: &event}},
	}, auth...)
	if err != nil {
		return providerError("post tool result", err)
	}
	return r.store.settle(call, w, []string{c.id})
}
