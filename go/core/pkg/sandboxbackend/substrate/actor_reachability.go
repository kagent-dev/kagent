package substrate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

const (
	defaultActorReachabilityTimeout  = 2 * time.Minute
	defaultActorReachabilityInterval = 500 * time.Millisecond
	actorReachabilityProbePath       = "/health"
)

type actorGetter interface {
	GetActor(ctx context.Context, actorID string) (*ateapipb.Actor, error)
}

// waitForActorReachableViaAtenet blocks until ate-api reports the actor RUNNING and
// atenet-router can reach its workload (non-5xx on /health with actor Host routing).
func waitForActorReachableViaAtenet(
	ctx context.Context,
	actors actorGetter,
	httpClient *http.Client,
	routerURL, actorID string,
) error {
	if actors == nil {
		return fmt.Errorf("substrate ate-api client is required")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return fmt.Errorf("actor id is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 3 * time.Second}
	}

	waitCtx, cancel := context.WithTimeout(ctx, defaultActorReachabilityTimeout)
	defer cancel()

	target, host, err := GatewayRouterTarget(routerURL, actorID)
	if err != nil {
		return err
	}
	probeURL := strings.TrimSuffix(target.String(), "/") + actorReachabilityProbePath

	ticker := time.NewTicker(defaultActorReachabilityInterval)
	defer ticker.Stop()

	for {
		actor, getErr := actors.GetActor(waitCtx, actorID)
		if getErr == nil && actor.GetStatus() == ateapipb.Actor_STATUS_RUNNING {
			statusCode, probeErr := probeActorViaAtenetRouter(waitCtx, httpClient, probeURL, host)
			if probeErr == nil && statusCode < http.StatusInternalServerError {
				return nil
			}
		}

		select {
		case <-waitCtx.Done():
			if getErr != nil {
				return fmt.Errorf("substrate session actor %q not reachable via atenet-router: %w", actorID, waitCtx.Err())
			}
			if actor != nil && actor.GetStatus() != ateapipb.Actor_STATUS_RUNNING {
				return fmt.Errorf(
					"substrate session actor %q not reachable via atenet-router: actor status %s: %w",
					actorID, ActorStatusLabel(actor.GetStatus()), waitCtx.Err(),
				)
			}
			return fmt.Errorf("substrate session actor %q not reachable via atenet-router: %w", actorID, waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func probeActorViaAtenetRouter(ctx context.Context, httpClient *http.Client, probeURL, actorHost string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return 0, err
	}
	req.Host = actorHost

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}
