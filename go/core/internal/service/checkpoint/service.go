package checkpoint

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

type store interface {
	ReserveAgentInstanceCheckpoint(context.Context, *apiv1alpha1.Checkpoint, string, string) (*apiv1alpha1.Checkpoint, *database.AgentInstanceTaskSnapshot, error)
	FinalizeAgentInstanceCheckpoint(context.Context, string, string, string, string) (*apiv1alpha1.Checkpoint, error)
	GetAgentInstanceCheckpoint(context.Context, string, string) (*apiv1alpha1.Checkpoint, error)
	ListAgentInstanceCheckpoints(context.Context, string, string, string, int) ([]*apiv1alpha1.Checkpoint, error)
	GetAgentInstanceCheckpointSnapshot(context.Context, string, string) (*database.AgentInstanceTaskSnapshot, string, error)
	BeginDeleteAgentInstanceCheckpoint(context.Context, string, string) (*database.AgentInstanceTaskSnapshot, string, error)
	DeleteAgentInstanceCheckpoint(context.Context, string, string) error
	ForkAgentInstance(context.Context, string, string, string, string) (*apiv1alpha1.AgentInstance, bool, error)
}

type workflow interface {
	Fork(context.Context, *apiv1alpha1.AgentInstance, *database.AgentInstanceTaskSnapshot, string) (*apiv1alpha1.AgentInstance, error)
}

type tagClient interface {
	GetActor(context.Context, string, string) (*ateapipb.Actor, error)
	GetTag(context.Context, string, string) (*ateapipb.Tag, error)
	CreateTag(context.Context, string, string, string) (*ateapipb.Tag, error)
	DeleteTag(context.Context, string, string) error
}

type Service struct {
	// ponytail: coalesce duplicate creates within one controller; use a durable
	// lease when multi-replica gateway coordination is supported.
	creates    singleflight.Group
	store      store
	authorizer auth.Authorizer
	tags       tagClient
	workflow   workflow
}

type ListRequest struct {
	InstanceID string
	PageSize   int
	PageToken  string
}

type ListResult struct {
	Checkpoints   []*apiv1alpha1.Checkpoint
	NextPageToken string
}

func NewService(store store, authorizer auth.Authorizer, tags tagClient, workflow workflow) *Service {
	return &Service{store: store, authorizer: authorizer, tags: tags, workflow: workflow}
}

func (s *Service) Create(ctx context.Context, instanceID, requestID string) (*apiv1alpha1.Checkpoint, error) {
	if err := validateCreate(instanceID, requestID); err != nil {
		return nil, err
	}
	userID, err := s.authorize(ctx, auth.VerbCreate, "AgentInstance", instanceID)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%q/%q/%q", userID, instanceID, requestID)
	result, err, _ := s.creates.Do(key, func() (any, error) {
		return s.create(ctx, userID, instanceID, requestID)
	})
	if err != nil {
		return nil, err
	}
	return result.(*apiv1alpha1.Checkpoint), nil
}

func (s *Service) create(ctx context.Context, userID, instanceID, requestID string) (*apiv1alpha1.Checkpoint, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to generate checkpoint identifier", err)
	}
	checkpoint, snapshot, err := s.store.ReserveAgentInstanceCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: id.String(), AgentInstanceId: instanceID}, userID, requestID)
	if errors.Is(err, database.ErrIdempotencyConflict) {
		return nil, serviceerrors.NewAlreadyExists("request_id was already used for a different checkpoint", err)
	}
	if errors.Is(err, database.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("AgentInstance not found", err)
	}
	if errors.Is(err, database.ErrAgentInstanceConflict) || errors.Is(err, database.ErrAgentInstanceNotQuiescent) {
		return nil, serviceerrors.NewFailedPrecondition("AgentInstance has no quiescent turn boundary", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to reserve checkpoint", err)
	}
	if checkpoint.State != apiv1alpha1.CheckpointState_CHECKPOINT_STATE_CREATING {
		return checkpoint, nil
	}

	tag, err := s.ensureTag(ctx, checkpoint, snapshot)
	if err != nil {
		cleanupErr := s.tags.DeleteTag(ctx, snapshot.Atespace, tagName(checkpoint.GetId()))
		if cleanupErr == nil || status.Code(cleanupErr) == codes.NotFound {
			_, finalizeErr := s.store.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.GetId(), "", "", err.Error())
			err = errors.Join(err, finalizeErr)
		} else {
			err = errors.Join(err, fmt.Errorf("cleanup checkpoint tag: %w", cleanupErr))
		}
		return nil, serviceerrors.NewUnavailable("Failed to retain checkpoint snapshot", err)
	}
	checkpoint, err = s.store.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.GetId(), tag.GetMetadata().GetUid(), tag.GetStatus().GetSnapshot().GetSnapshotUri(), "")
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to publish checkpoint", err)
	}
	return checkpoint, nil
}

// The CREATING reservation blocks task admission and lifecycle changes while
// CreateTag copies the Actor's current snapshot. Verify both sides of the copy
// because ate-api also permits Actors to be changed outside kagent.
func (s *Service) ensureTag(ctx context.Context, checkpoint *apiv1alpha1.Checkpoint, reference *database.AgentInstanceTaskSnapshot) (*ateapipb.Tag, error) {
	actorName := substrate.ActorName(checkpoint.GetAgentInstanceId())
	actor, err := s.verifySnapshot(ctx, actorName, reference)
	if err != nil {
		return nil, err
	}
	name := tagName(checkpoint.GetId())
	tag, err := s.tags.CreateTag(ctx, reference.Atespace, name, actorName)
	if err != nil {
		tag, err = s.tags.GetTag(ctx, reference.Atespace, name)
		if err != nil {
			return nil, fmt.Errorf("create snapshot tag: %w", err)
		}
	}
	metadata, source, snapshot := tag.GetMetadata(), tag.GetSourceActor(), tag.GetStatus().GetSnapshot()
	if metadata.GetAtespace() != reference.Atespace || metadata.GetName() != name || metadata.GetUid() == "" ||
		source.GetAtespace() != reference.Atespace || source.GetName() != actorName ||
		tag.GetStatus().GetSourceActorUid() != actor.GetMetadata().GetUid() ||
		tag.GetStatus().GetActorTemplateUid() == "" || snapshot.GetSnapshotUri() == "" ||
		strings.TrimPrefix(snapshot.GetContentScope().String(), "SNAPSHOT_CONTENT_SCOPE_") != reference.ContentScope ||
		tag.GetScope() != ateapipb.TagScope_TAG_SCOPE_ATESPACE {
		return nil, fmt.Errorf("snapshot tag %s/%s returned invalid identity", reference.Atespace, name)
	}
	verified, err := s.verifySnapshot(ctx, actorName, reference)
	if err != nil {
		return nil, err
	}
	if verified.GetMetadata().GetUid() != actor.GetMetadata().GetUid() {
		return nil, fmt.Errorf("checkpoint Actor identity changed while copying snapshot")
	}
	return tag, nil
}

func (s *Service) verifySnapshot(ctx context.Context, actorName string, reference *database.AgentInstanceTaskSnapshot) (*ateapipb.Actor, error) {
	actor, err := s.tags.GetActor(ctx, reference.Atespace, actorName)
	if err != nil {
		return nil, fmt.Errorf("get checkpoint Actor: %w", err)
	}
	metadata, snapshot := actor.GetMetadata(), actor.GetStatus().GetExternalSnapshot()
	if metadata.GetAtespace() != reference.Atespace || metadata.GetName() != actorName || metadata.GetUid() == "" ||
		actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED ||
		reference.URI == "" || snapshot.GetSnapshotUri() != reference.URI {
		return nil, fmt.Errorf("checkpoint Actor %s/%s snapshot changed", reference.Atespace, actorName)
	}
	if scope := strings.TrimPrefix(snapshot.GetContentScope().String(), "SNAPSHOT_CONTENT_SCOPE_"); scope != reference.ContentScope {
		return nil, fmt.Errorf("checkpoint Actor %s/%s snapshot content scope changed", reference.Atespace, actorName)
	}
	return actor, nil
}

func (s *Service) Get(ctx context.Context, checkpointID string) (*apiv1alpha1.Checkpoint, error) {
	if err := validateIdentity(checkpointID); err != nil {
		return nil, err
	}
	userID, err := s.authorize(ctx, auth.VerbGet, "Checkpoint", checkpointID)
	if err != nil {
		return nil, err
	}
	checkpoint, err := s.store.GetAgentInstanceCheckpoint(ctx, checkpointID, userID)
	if errors.Is(err, database.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get checkpoint", err)
	}
	return checkpoint, nil
}

func (s *Service) List(ctx context.Context, request ListRequest) (ListResult, error) {
	if err := validateIdentity(request.InstanceID); err != nil {
		return ListResult{}, err
	}
	userID, err := s.authorize(ctx, auth.VerbGet, "Checkpoint", request.InstanceID)
	if err != nil {
		return ListResult{}, err
	}
	pageSize := request.PageSize
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if pageSize < 0 || pageSize > maxPageSize {
		return ListResult{}, serviceerrors.NewInvalidArgument(fmt.Sprintf("page limit must be between 1 and %d", maxPageSize), nil)
	}
	afterID, err := decodePageToken(request.PageToken)
	if err != nil {
		return ListResult{}, serviceerrors.NewInvalidArgument("page token is invalid", err)
	}
	rows, err := s.store.ListAgentInstanceCheckpoints(ctx, request.InstanceID, userID, afterID, pageSize+1)
	if err != nil {
		return ListResult{}, serviceerrors.NewInternal("Failed to list checkpoints", err)
	}
	result := ListResult{Checkpoints: rows[:min(len(rows), pageSize)]}
	if len(rows) > pageSize {
		result.NextPageToken = encodePageToken(rows[pageSize-1].GetId())
	}
	return result, nil
}

func (s *Service) Delete(ctx context.Context, checkpointID string) error {
	if err := validateIdentity(checkpointID); err != nil {
		return err
	}
	userID, err := s.authorize(ctx, auth.VerbDelete, "Checkpoint", checkpointID)
	if err != nil {
		return err
	}
	snapshot, tagUID, err := s.store.BeginDeleteAgentInstanceCheckpoint(ctx, checkpointID, userID)
	if errors.Is(err, database.ErrNotFound) {
		return serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return serviceerrors.NewInternal("Failed to begin checkpoint deletion", err)
	}
	tag, err := s.tags.GetTag(ctx, snapshot.Atespace, tagName(checkpointID))
	if err != nil && status.Code(err) != codes.NotFound {
		return serviceerrors.NewUnavailable("Failed to get checkpoint snapshot tag", err)
	}
	if err == nil && (tag.GetMetadata().GetUid() != tagUID ||
		tag.GetMetadata().GetAtespace() != snapshot.Atespace || tag.GetMetadata().GetName() != tagName(checkpointID) ||
		tag.GetStatus().GetSnapshot().GetSnapshotUri() != snapshot.URI) {
		return serviceerrors.NewFailedPrecondition("Checkpoint snapshot tag identity changed", nil)
	}
	if err := s.tags.DeleteTag(ctx, snapshot.Atespace, tagName(checkpointID)); err != nil && status.Code(err) != codes.NotFound {
		return serviceerrors.NewUnavailable("Failed to delete checkpoint snapshot tag", err)
	}
	if err := s.store.DeleteAgentInstanceCheckpoint(ctx, checkpointID, userID); err != nil {
		return serviceerrors.NewInternal("Failed to delete checkpoint", err)
	}
	return nil
}

func (s *Service) Fork(ctx context.Context, checkpointID, requestID string) (*apiv1alpha1.AgentInstance, error) {
	if err := validateCreate(checkpointID, requestID); err != nil {
		return nil, err
	}
	userID, err := s.authorize(ctx, auth.VerbCreate, "AgentInstance", "")
	if err != nil {
		return nil, err
	}
	snapshot, tagUID, err := s.store.GetAgentInstanceCheckpointSnapshot(ctx, checkpointID, userID)
	if errors.Is(err, database.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get checkpoint", err)
	}
	if snapshot.ContentScope != "DATA" {
		return nil, serviceerrors.NewFailedPrecondition("Checkpoint includes process state and cannot be forked", nil)
	}
	tag, err := s.tags.GetTag(ctx, snapshot.Atespace, tagName(checkpointID))
	if err != nil {
		return nil, serviceerrors.NewUnavailable("Failed to get checkpoint tag", err)
	}
	if tagUID == "" || tag.GetMetadata().GetUid() != tagUID ||
		tag.GetMetadata().GetAtespace() != snapshot.Atespace || tag.GetMetadata().GetName() != tagName(checkpointID) ||
		snapshot.URI == "" || tag.GetStatus().GetSnapshot().GetSnapshotUri() != snapshot.URI ||
		tag.GetStatus().GetSnapshot().GetContentScope() != ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA {
		return nil, serviceerrors.NewFailedPrecondition("Checkpoint tag identity changed", nil)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to generate AgentInstance identifier", err)
	}
	instance, _, err := s.store.ForkAgentInstance(ctx, checkpointID, userID, requestID, id.String())
	if errors.Is(err, database.ErrIdempotencyConflict) {
		return nil, serviceerrors.NewAlreadyExists("request_id was already used for a different AgentInstance", err)
	}
	if errors.Is(err, database.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to reserve fork AgentInstance", err)
	}
	instance, err = s.workflow.Fork(ctx, instance, snapshot, tagName(checkpointID))
	if err != nil {
		return nil, serviceerrors.NewUnavailable("Failed to create fork AgentInstance", err)
	}
	return instance, nil
}

func (s *Service) authorize(ctx context.Context, verb auth.Verb, resourceType, name string) (string, error) {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return "", serviceerrors.NewUnauthenticated("Failed to get authenticated principal", nil)
	}
	principal := session.Principal()
	if err := s.authorizer.Check(ctx, principal, verb, auth.Resource{Type: resourceType, Name: name}); err != nil {
		return "", serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	return principal.User.ID, nil
}

func validateCreate(instanceID, requestID string) error {
	if err := validateIdentity(instanceID); err != nil {
		return err
	}
	if requestID == "" || strings.TrimSpace(requestID) != requestID || len(requestID) > 128 {
		return serviceerrors.NewInvalidArgument("request_id must be 1-128 characters without surrounding whitespace", nil)
	}
	return nil
}

func validateIdentity(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return serviceerrors.NewInvalidArgument("identifier is invalid", err)
	}
	return nil
}

func encodePageToken(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id))
}

func tagName(checkpointID string) string { return "checkpoint-" + checkpointID }

func decodePageToken(token string) (string, error) {
	if token == "" {
		return "", nil
	}
	value, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", err
	}
	if _, err := uuid.Parse(string(value)); err != nil {
		return "", err
	}
	return string(value), nil
}
