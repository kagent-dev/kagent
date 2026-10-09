package session

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// validatePushConfig checks the fields supported by the OSS callback sender.
func validatePushConfig(agent types.NamespacedName, input *a2a.PushConfig) (*a2a.PushConfig, error) {
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
	if _, err := validatePushURL(config.URL, kagentenv.A2APushAllowHTTP.Get()); err != nil {
		return nil, a2a.NewError(a2a.ErrInvalidParams, err.Error())
	}
	return &config, nil
}

func validatePushURL(raw string, allowHTTP bool) (*url.URL, error) {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Hostname() == "" || (endpoint.Scheme != "https" && (endpoint.Scheme != "http" || !allowHTTP)) || endpoint.User != nil || endpoint.Fragment != "" || strings.TrimSpace(raw) != raw {
		return nil, fmt.Errorf("push URL must be an absolute HTTPS URL without credentials (HTTP requires KAGENT_A2A_PUSH_ALLOW_HTTP)")
	}
	return endpoint, nil
}

func initialPushConfig(agent types.NamespacedName, req *a2a.SendMessageRequest) (*a2a.PushConfig, error) {
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
	return validatePushConfig(agent, req.Config.PushConfig)
}

type pushStore interface {
	ExpireUnboundPushRegistrations(context.Context) error
	ClaimDuePushDelivery(context.Context) (*database.PushDelivery, error)
	FinishPushDelivery(context.Context, database.PushDelivery, bool) error
}

type pushSender interface {
	SendPush(context.Context, *a2a.PushConfig, a2a.Event) error
}

// PushWorker sends committed outbox payloads. Lease expiry permits another
// elected worker to retry after a crash.
type PushWorker struct {
	store  pushStore
	sender pushSender
	signer *PushJWTSigner
}

var _ manager.LeaderElectionRunnable = (*PushWorker)(nil)
var _ manager.Runnable = (*PushWorker)(nil)
var _ pushStore = (*database.Client)(nil)

func NewPushWorker(store pushStore, sender pushSender, signer *PushJWTSigner) *PushWorker {
	return &PushWorker{store: store, sender: sender, signer: signer}
}

func (p *PushWorker) NeedLeaderElection() bool { return true }

func (p *PushWorker) Start(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		fullBatch, err := p.poll(ctx)
		if err != nil && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to process push notifications", "error", err)
		}
		if fullBatch {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// poll reports whether it filled the batch, so a backlog can drain without
// waiting for the idle polling interval.
func (p *PushWorker) poll(ctx context.Context) (bool, error) {
	// Bound each pass so an always-due backlog cannot starve input maintenance.
	claimed := 0
	for range 100 {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		delivery, err := p.store.ClaimDuePushDelivery(ctx)
		if err != nil {
			return false, err
		}
		if delivery == nil {
			break
		}
		claimed++
		if err := p.send(ctx, *delivery); err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "push delivery attempt failed", "notification_id", delivery.ID, "error", err)
		}
	}
	return claimed == 100, p.store.ExpireUnboundPushRegistrations(ctx)
}

func (p *PushWorker) send(ctx context.Context, delivery database.PushDelivery) error {
	// Use the event and destination captured at publication
	wire := &a2apb.StreamResponse{}
	if err := proto.Unmarshal(delivery.Payload, wire); err != nil {
		return fmt.Errorf("decode durable notification: %w", err)
	}
	event, err := pbconv.FromProtoStreamResponse(wire)
	if err != nil {
		return fmt.Errorf("convert durable notification: %w", err)
	}
	config := &a2a.PushConfig{URL: delivery.URL}
	credential, err := p.signer.Sign(delivery.TaskID, delivery.URL)
	if err != nil {
		return fmt.Errorf("sign push notification: %w", err)
	}
	config.Auth = &a2a.PushAuthInfo{Scheme: "Bearer", Credentials: credential}
	sendErr := p.sender.SendPush(ctx, config, event)
	// A failed HTTP attempt remains durable for the store's retry schedule.
	if err := p.store.FinishPushDelivery(ctx, delivery, sendErr == nil); err != nil {
		return fmt.Errorf("persist push delivery outcome: %w", err)
	}
	return sendErr
}
