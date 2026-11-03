package session

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// validatePushConfig checks the fields supported by the OSS callback sender.
func validatePushConfig(agent types.NamespacedName, input *a2a.PushConfig, allowHTTP bool) (*a2a.PushConfig, error) {
	if input == nil {
		return nil, a2a.ErrInvalidParams
	}
	config := *input
	if config.Token != "" {
		return nil, a2a.NewError(a2a.ErrInvalidParams, "push notification token is not supported; verify the signed JWT instead")
	}
	if config.Auth != nil && (!strings.EqualFold(config.Auth.Scheme, "Bearer") || config.Auth.Credentials != "") {
		return nil, a2a.NewError(a2a.ErrInvalidParams, "push authentication supports Bearer with server-signed JWTs; do not supply credentials")
	}
	if config.Tenant != "" && config.Tenant != agent.Namespace+"/"+agent.Name {
		return nil, a2a.NewError(a2a.ErrInvalidParams, "push tenant does not match Agent route")
	}
	if _, err := validatePushURL(config.URL, allowHTTP); err != nil {
		return nil, a2a.NewError(a2a.ErrInvalidParams, err.Error())
	}
	return &config, nil
}

func validatePushURL(raw string, allowHTTP bool) (*url.URL, error) {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Hostname() == "" || (endpoint.Scheme != "https" && (endpoint.Scheme != "http" || !allowHTTP)) || endpoint.User != nil || endpoint.Fragment != "" || strings.TrimSpace(raw) != raw {
		return nil, fmt.Errorf("push URL must be an absolute HTTPS URL without credentials (HTTP requires push.allowHttp)")
	}
	return endpoint, nil
}

func initialPushConfig(agent types.NamespacedName, req *a2a.SendMessageRequest, allowHTTP bool) (*a2a.PushConfig, error) {
	if req == nil || req.Config == nil || req.Config.PushConfig == nil {
		return nil, nil
	}
	if req.Message == nil || req.Message.ID == "" {
		return nil, a2a.NewError(a2a.ErrInvalidParams, "push configuration requires a message ID")
	}
	// The enclosing message identifies the task, if known. The embedded
	// config itself is only a destination and must not name another task.
	if req.Config.PushConfig.TaskID != "" {
		return nil, a2a.NewError(a2a.ErrInvalidParams, "embedded push taskId is unsupported")
	}
	return validatePushConfig(agent, req.Config.PushConfig, allowHTTP)
}

type pushStore interface {
	ExpireUnboundPushRegistrations(context.Context) error
	ClaimDuePushDelivery(context.Context, time.Duration) (*database.PushDelivery, error)
	FinishPushDelivery(context.Context, database.PushDelivery, bool) error
	ReleasePushDelivery(context.Context, database.PushDelivery) error
	IsPushDeliveryCurrent(context.Context, database.PushDelivery) (bool, error)
	HasPendingPushDelivery(context.Context) (bool, error)
}

type pushSender interface {
	SendPush(context.Context, *a2a.PushConfig, a2a.Event) error
}

// PushWorker sends current published status updates on every API replica. PostgreSQL
// claims coordinate workers; lease expiry permits recovery after a crash.
type PushWorker struct {
	store  pushStore
	sender pushSender
	signer *PushJWTSigner
	wake   <-chan struct{}
	config PushWorkerConfig
}

// PushWorkerConfig contains resolved, positive controller delivery settings.
type PushWorkerConfig struct {
	RecoveryInterval time.Duration
	MaxConcurrency   int
	SendTimeout      time.Duration
}

var _ manager.LeaderElectionRunnable = (*PushWorker)(nil)
var _ manager.Runnable = (*PushWorker)(nil)
var _ pushStore = (*database.Client)(nil)

// NewPushWorker sends unsigned notifications when signer is nil.
func NewPushWorker(store pushStore, sender pushSender, signer *PushJWTSigner, wake <-chan struct{}, config PushWorkerConfig) *PushWorker {
	return &PushWorker{store: store, sender: sender, signer: signer, wake: wake, config: config}
}

func (p *PushWorker) NeedLeaderElection() bool { return false }

// Start drains work at startup and after local settlement hints. Recovery scans
// find missed hints; outstanding retries and leases retain the five-second scan.
func (p *PushWorker) Start(ctx context.Context) error {
	timer := time.NewTimer(p.config.RecoveryInterval)
	defer timer.Stop()
	cleanupTimer := time.NewTimer(p.config.RecoveryInterval)
	defer cleanupTimer.Stop()
	cleanup := func() {
		delay := p.config.RecoveryInterval
		if err := p.store.ExpireUnboundPushRegistrations(ctx); err != nil && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to expire push registrations", "error", err)
			delay = min(time.Second, delay)
		}
		cleanupTimer.Reset(delay)
	}
	cleanup()
	completed := make(chan struct{}, p.config.MaxConcurrency)
	active := 0
	var sends sync.WaitGroup
	defer sends.Wait()
	dispatch := func(delivery database.PushDelivery) {
		active++
		sends.Go(func() {
			if err := p.send(ctx, delivery); err != nil {
				logging.FromContext(ctx).ErrorContext(ctx, "push delivery attempt failed", "registration_id", delivery.ID, "event_sequence", delivery.Sequence, "error", err)
			}
			completed <- struct{}{}
		})
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		// Keep maintenance periodic even when a large backlog drains without waiting.
		select {
		case <-cleanupTimer.C:
			cleanup()
		default:
		}
		// Only this goroutine receives completions. Drain the buffered snapshot
		// before calculating capacity, including when a wake or timer won the select.
		for range len(completed) {
			<-completed
			active--
		}
		// Claim only for available slots: queued claims would consume their leases
		// while waiting for a slow receiver. Completion immediately frees a slot.
		fullBatch, err := p.poll(ctx, p.config.MaxConcurrency-active, dispatch)
		if fullBatch && err == nil {
			continue
		}
		delay := p.config.RecoveryInterval
		if err == nil && active < p.config.MaxConcurrency {
			var pending bool
			pending, err = p.store.HasPendingPushDelivery(ctx)
			// If there are pending deliveries, we want to poll more frequently to avoid delays in processing.
			if pending {
				delay = min(5*time.Second, p.config.RecoveryInterval)
			}
		}
		if err != nil && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to process push notifications", "error", err)
			delay = min(time.Second, p.config.RecoveryInterval)
		}
		// Saturated workers need no pending-work queries or recovery scans until
		// capacity is freed. Registration cleanup has its own timer.
		var scan <-chan time.Time
		if active < p.config.MaxConcurrency || err != nil {
			timer.Reset(delay)
			scan = timer.C
		}
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-cleanupTimer.C:
				cleanup()
				continue
			case <-scan:
			case <-p.wake:
			case <-completed:
				active--
			}
			break
		}
	}
}

// poll reports whether it filled the batch, so a backlog can drain without
// waiting for the idle polling interval.
func (p *PushWorker) poll(ctx context.Context, available int, dispatch func(database.PushDelivery)) (bool, error) {
	// Bound each pass so an always-due backlog cannot starve registration cleanup.
	claimed := 0
	for range min(100, available) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		delivery, err := p.store.ClaimDuePushDelivery(ctx, p.config.SendTimeout)
		if err != nil {
			return false, err
		}
		if delivery == nil {
			break
		}
		claimed++
		dispatch(*delivery)
	}
	return claimed == 100, nil
}

func (p *PushWorker) send(ctx context.Context, delivery database.PushDelivery) error {
	sendCtx, cancel := context.WithTimeout(ctx, p.config.SendTimeout)
	delivered, sendErr := p.attempt(sendCtx, delivery)
	cancel()
	if !delivered && sendErr == nil {
		return nil // A superseded claim must not alter newer work.
	}
	// Preserve known outcomes during shutdown, and bound finalization even if
	// the database is unavailable. A canceled send releases its retry budget.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	var finishErr error
	if ctx.Err() != nil && errors.Is(sendErr, ctx.Err()) {
		finishErr = p.store.ReleasePushDelivery(finishCtx, delivery)
	} else {
		finishErr = p.store.FinishPushDelivery(finishCtx, delivery, delivered)
	}
	if finishErr != nil {
		return errors.Join(sendErr, fmt.Errorf("failed to persist push delivery outcome: %w", finishErr))
	}
	return sendErr
}

// attempt returns false without an error only when the claim is no longer current.
func (p *PushWorker) attempt(ctx context.Context, delivery database.PushDelivery) (bool, error) {
	// Use the published status and destination captured by this claim.
	wire := &a2apb.StreamResponse{}
	if err := proto.Unmarshal(delivery.Payload, wire); err != nil {
		return false, fmt.Errorf("decode durable notification: %w", err)
	}
	event, err := pbconv.FromProtoStreamResponse(wire)
	if err != nil {
		return false, fmt.Errorf("convert durable notification: %w", err)
	}
	config := &a2a.PushConfig{URL: delivery.URL}
	if p.signer != nil {
		credential, err := p.signer.Sign(delivery.TaskID, delivery.URL)
		if err != nil {
			return false, fmt.Errorf("sign push notification: %w", err)
		}
		config.Auth = &a2a.PushAuthInfo{Scheme: "Bearer", Credentials: credential}
	}
	current, err := p.store.IsPushDeliveryCurrent(ctx, delivery)
	if err != nil {
		return false, fmt.Errorf("failed to check push registration %s: %w", delivery.ID, err)
	}
	if !current {
		return false, nil
	}
	err = p.sender.SendPush(ctx, config, event)
	return err == nil, err
}
