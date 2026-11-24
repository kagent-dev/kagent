package anthropicworker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jackc/pgx/v5"

	"github.com/kagent-dev/kagent/go/pkg/logging"
)

var errSessionEnded = errors.New("provider session ended")
var errHeartbeatRejected = errors.New("provider heartbeat rejected; waiting for redispatch")
var errResultRetryable = errors.New("temporary tool result delivery failure")

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
	logger := logging.FromContext(ctx).With("environment_id", env.ID)
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
			logger.DebugContext(ctx, "anthropic poll returned no work", "http_status", raw.StatusCode)
			wait(ctx, tickInterval)
			continue
		}
		if err != nil {
			r.warn(ctx, "poll assignment", providerError("poll", err))
			wait(ctx, retryDelay)
			continue
		}
		if work == nil || work.ID == "" || work.Data.ID == "" {
			status := 0
			if raw != nil {
				status = raw.StatusCode
			}
			logger.DebugContext(ctx, "anthropic poll returned no usable work", "http_status", status,
				"has_work_id", work != nil && work.ID != "", "has_session_id", work != nil && work.Data.ID != "")
			wait(ctx, tickInterval)
			continue
		}
		token := sessionsToken(work.Secret)
		logger.InfoContext(ctx, "anthropic poll offered work", "work_id", work.ID, "has_session_credential", token != "")
		call, cancel = context.WithTimeout(ctx, dbTimeout)
		err = r.acceptOffer(call, assignment{id: work.ID, environment: env.ID, sessionID: work.Data.ID}, token)
		cancel()
		if err != nil {
			r.warn(ctx, "persist assignment", err)
		}
		// Poll can return an unacknowledged assignment again immediately.
		wait(ctx, tickInterval)
	}
}

func (r *Runner) acceptOffer(ctx context.Context, w assignment, token string) error {
	w.credential = r.cipher.Seal(nil, nil, []byte(token), []byte(w.id))
	if err := r.store.enqueue(ctx, w); err != nil || token == "" {
		return err
	}
	previous, err := r.store.parkedCredential(ctx, w)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	decoded, err := r.cipher.Open(nil, nil, previous, []byte(w.id))
	if err != nil {
		return errors.New("decrypt parked assignment credential")
	}
	if string(decoded) == token {
		return nil
	}
	return r.store.refreshCredential(ctx, w, previous)
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

func (r *Runner) heartbeat(ctx context.Context, w assignment, auth []option.RequestOption, ready chan<- struct{}, stopRelay context.CancelFunc) error {
	expected := w.heartbeat
	if expected == "" {
		expected = "NO_HEARTBEAT"
	}
	first := true
	var leaseDeadline time.Time
	backoff := tickInterval
	for ctx.Err() == nil {
		started := time.Now()
		deadline := leaseDeadline
		if apiDeadline := started.Add(apiTimeout); deadline.IsZero() || apiDeadline.Before(deadline) {
			deadline = apiDeadline
		}
		if !started.Before(deadline) {
			stopRelay()
			return nil
		}
		// Complete an in-flight heartbeat and save its continuation token on shutdown.
		call, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
		response, err := r.provider.Beta.Environments.Work.Heartbeat(call, w.id, anthropic.BetaEnvironmentWorkHeartbeatParams{
			EnvironmentID: w.environment, ExpectedLastHeartbeat: anthropic.String(expected),
		}, auth...)
		cancel()
		if providerStatus(err, 404) || providerStatus(err, 410) ||
			(response != nil && response.State == anthropic.BetaSelfHostedWorkHeartbeatResponseStateStopped) {
			// A stopped Work item doesn't close the session.
			if first {
				close(ready)
			}
			return nil
		}
		if err != nil {
			if providerStatus(err, http.StatusUnauthorized) || providerStatus(err, http.StatusPreconditionFailed) {
				// A changed continuation token can belong to another provider worker.
				return fmt.Errorf("%w: %v", errHeartbeatRejected, providerError("heartbeat", err))
			}
			if transientProviderError(err) && !leaseDeadline.IsZero() {
				wait(ctx, min(backoff, time.Until(leaseDeadline)))
				backoff = min(backoff*2, retryDelay)
				continue
			}
			return providerError("heartbeat", err)
		}
		if response == nil || !response.LeaseExtended || response.LastHeartbeat == "" || response.TTLSeconds <= 0 {
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
			first = false
		}
		ttl := time.Duration(response.TTLSeconds) * time.Second
		leaseDeadline = started.Add(ttl)
		backoff = tickInterval
		wait(ctx, min(time.Duration(max(1, min(15, response.TTLSeconds/3)))*time.Second, time.Until(leaseDeadline)))
	}
	return ctx.Err()
}

func transientProviderError(err error) bool {
	var upstream *anthropic.Error
	if errors.As(err, &upstream) {
		return upstream.StatusCode == http.StatusRequestTimeout || upstream.StatusCode == http.StatusTooManyRequests ||
			upstream.StatusCode >= http.StatusInternalServerError
	}
	var transport *url.Error
	return errors.As(err, &transport) || errors.Is(err, context.DeadlineExceeded)
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
		if transientProviderError(err) {
			return fmt.Errorf("%w: %w", errResultRetryable, providerError("post tool result", err))
		}
		return providerError("post tool result", err)
	}
	return r.store.settle(call, w, []string{c.id})
}
