package controller

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

type actorEgressStore interface {
	ListRuntimeEgressSourceTargets(context.Context, database.RuntimeEgressSource, string, int) ([]database.RuntimeEgressTarget, error)
}

type actorEgressClient interface {
	GetActor(context.Context, string, string) (*ateapipb.Actor, error)
	EnsureActorEgressPolicy(context.Context, string, string, string) error
}

// applySourceEgress updates every allocated actor, including suspended actors and
// former source UIDs. Preparation retries the source on failure, even when its
// grants are unchanged. Continue past individual failures so one unavailable
// actor cannot prevent revocation on the others.
func applySourceEgress(ctx context.Context, store actorEgressStore, actors actorEgressClient, source database.RuntimeEgressSource) error {
	const pageSize = 100
	var failures *multierror.Error
	after := ""
	for {
		targets, err := store.ListRuntimeEgressSourceTargets(ctx, source, after, pageSize)
		if err != nil {
			return multierror.Append(failures, fmt.Errorf("failed to list %s %s/%s actors: %w", source.Kind, source.Namespace, source.Name, err)).ErrorOrNil()
		}
		var group errgroup.Group
		group.SetLimit(16)
		for _, target := range targets {
			group.Go(func() error {
				if err := applyActorEgress(ctx, actors, target); err != nil {
					return fmt.Errorf("failed to apply actor %s egress: %w", target.ID, err)
				}
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			failures = multierror.Append(failures, err)
		}
		if len(targets) < pageSize {
			return failures.ErrorOrNil()
		}
		after = targets[len(targets)-1].ID
	}
}

func applyActorEgress(ctx context.Context, actors actorEgressClient, target database.RuntimeEgressTarget) error {
	binding := substrate.ActorBinding{Atespace: target.Atespace, Name: substrate.ActorName(target.ID), TemplateAtespace: target.Atespace, TemplateName: target.TemplateName}
	actor, err := actors.GetActor(ctx, binding.Atespace, binding.Name)
	if status.Code(err) == codes.NotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if !binding.Matches(actor) || (target.ActorUID != "" && target.ActorUID != actor.GetMetadata().GetUid()) {
		return nil
	}
	err = actors.EnsureActorEgressPolicy(ctx, binding.Atespace, binding.Name, target.Revision)
	if status.Code(err) == codes.NotFound {
		return nil
	}
	return err
}
