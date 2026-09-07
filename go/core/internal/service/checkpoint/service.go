package checkpoint

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

type store interface {
	ReserveAgentInstanceCheckpoint(context.Context, dbpkg.AgentInstanceCheckpoint) (*dbpkg.AgentInstanceCheckpoint, error)
	FinalizeAgentInstanceCheckpoint(context.Context, string, string, string) (*dbpkg.AgentInstanceCheckpoint, error)
	GetAgentInstanceCheckpoint(context.Context, string, string) (*dbpkg.AgentInstanceCheckpoint, error)
	ListAgentInstanceCheckpoints(context.Context, string, string, string, int) ([]dbpkg.AgentInstanceCheckpoint, error)
	BeginDeleteAgentInstanceCheckpoint(context.Context, string, string) (*dbpkg.AgentInstanceCheckpoint, error)
	DeleteAgentInstanceCheckpoint(context.Context, string, string) error
	ForkAgentInstance(context.Context, string, string, string, string) (*apiv1alpha1.AgentInstance, bool, error)
}

type workflow interface {
	Fork(context.Context, *apiv1alpha1.AgentInstance, *dbpkg.AgentInstanceCheckpoint) (*apiv1alpha1.AgentInstance, error)
}

type tagClient interface {
	GetActorSnapshot(context.Context, string, string) (*ateapipb.ActorSnapshot, error)
	GetActorSnapshotTag(context.Context, string, string) (*ateapipb.ActorSnapshotTag, error)
	CreateActorSnapshotTag(context.Context, string, string, string) (*ateapipb.ActorSnapshotTag, error)
	DeleteActorSnapshotTag(context.Context, string, string) error
}

type Service struct {
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
	id, err := uuid.NewV7()
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to generate checkpoint identifier", err)
	}
	checkpoint, err := s.store.ReserveAgentInstanceCheckpoint(ctx, dbpkg.AgentInstanceCheckpoint{
		Checkpoint: &apiv1alpha1.Checkpoint{Id: id.String(), AgentInstanceId: instanceID}, UserID: userID,
		RequestID: requestID,
	})
	if errors.Is(err, dbpkg.ErrIdempotencyConflict) {
		return nil, serviceerrors.NewAlreadyExists("request_id was already used for a different checkpoint", err)
	}
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("AgentInstance not found", err)
	}
	if errors.Is(err, dbpkg.ErrAgentInstanceConflict) || errors.Is(err, dbpkg.ErrAgentInstanceNotQuiescent) {
		return nil, serviceerrors.NewFailedPrecondition("AgentInstance has no quiescent turn boundary", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to reserve checkpoint", err)
	}
	if checkpoint.State != apiv1alpha1.CheckpointState_CHECKPOINT_STATE_CREATING {
		return checkpoint.Checkpoint, nil
	}

	tag, err := s.ensureTag(ctx, checkpoint)
	if err != nil {
		cleanupErr := s.tags.DeleteActorSnapshotTag(ctx, checkpoint.SnapshotAtespace, tagName(checkpoint.GetId()))
		if cleanupErr == nil || status.Code(cleanupErr) == codes.NotFound {
			_, _ = s.store.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.GetId(), "", err.Error())
		}
		return nil, serviceerrors.NewUnavailable("Failed to retain checkpoint snapshot", err)
	}
	checkpoint, err = s.store.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.GetId(), tag.GetMetadata().GetUid(), "")
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to publish checkpoint", err)
	}
	return checkpoint.Checkpoint, nil
}

func (s *Service) ensureTag(ctx context.Context, checkpoint *dbpkg.AgentInstanceCheckpoint) (*ateapipb.ActorSnapshotTag, error) {
	if err := s.verifySnapshot(ctx, checkpoint); err != nil {
		return nil, err
	}
	name := tagName(checkpoint.GetId())
	tag, err := s.tags.CreateActorSnapshotTag(ctx, checkpoint.SnapshotAtespace, name, checkpoint.SnapshotName)
	if err != nil {
		tag, err = s.tags.GetActorSnapshotTag(ctx, checkpoint.SnapshotAtespace, name)
		if err != nil {
			return nil, fmt.Errorf("create snapshot tag: %w", err)
		}
	}
	metadata, snapshot := tag.GetMetadata(), tag.GetSnapshot()
	if metadata.GetAtespace() != checkpoint.SnapshotAtespace || metadata.GetName() != name || metadata.GetUid() == "" ||
		snapshot.GetAtespace() != checkpoint.SnapshotAtespace || snapshot.GetName() != checkpoint.SnapshotName ||
		tag.GetScope() != ateapipb.ActorSnapshotTagScope_ACTOR_SNAPSHOT_TAG_SCOPE_ATESPACE {
		return nil, fmt.Errorf("snapshot tag %s/%s returned invalid identity", checkpoint.SnapshotAtespace, name)
	}
	if err := s.verifySnapshot(ctx, checkpoint); err != nil {
		return nil, err
	}
	return tag, nil
}

func (s *Service) verifySnapshot(ctx context.Context, checkpoint *dbpkg.AgentInstanceCheckpoint) error {
	snapshot, err := s.tags.GetActorSnapshot(ctx, checkpoint.SnapshotAtespace, checkpoint.SnapshotName)
	if err != nil {
		return fmt.Errorf("get checkpoint snapshot: %w", err)
	}
	metadata := snapshot.GetMetadata()
	if metadata.GetAtespace() != checkpoint.SnapshotAtespace || metadata.GetName() != checkpoint.SnapshotName || metadata.GetUid() != checkpoint.SnapshotUID {
		return fmt.Errorf("checkpoint snapshot %s/%s identity changed", checkpoint.SnapshotAtespace, checkpoint.SnapshotName)
	}
	if scope := strings.TrimPrefix(snapshot.GetStatus().GetContentScope().String(), "SNAPSHOT_CONTENT_SCOPE_"); scope != checkpoint.SnapshotContentScope {
		return fmt.Errorf("checkpoint snapshot %s/%s content scope changed", checkpoint.SnapshotAtespace, checkpoint.SnapshotName)
	}
	return nil
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
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get checkpoint", err)
	}
	return checkpoint.Checkpoint, nil
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
	result := ListResult{Checkpoints: make([]*apiv1alpha1.Checkpoint, min(len(rows), pageSize))}
	for i := range result.Checkpoints {
		result.Checkpoints[i] = rows[i].Checkpoint
	}
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
	checkpoint, err := s.store.BeginDeleteAgentInstanceCheckpoint(ctx, checkpointID, userID)
	if errors.Is(err, dbpkg.ErrNotFound) {
		return serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return serviceerrors.NewInternal("Failed to begin checkpoint deletion", err)
	}
	tag, err := s.tags.GetActorSnapshotTag(ctx, checkpoint.SnapshotAtespace, tagName(checkpoint.GetId()))
	if err != nil && status.Code(err) != codes.NotFound {
		return serviceerrors.NewUnavailable("Failed to get checkpoint snapshot tag", err)
	}
	if err == nil && (tag.GetMetadata().GetUid() != checkpoint.TagUID ||
		tag.GetSnapshot().GetAtespace() != checkpoint.SnapshotAtespace || tag.GetSnapshot().GetName() != checkpoint.SnapshotName) {
		return serviceerrors.NewFailedPrecondition("Checkpoint snapshot tag identity changed", nil)
	}
	if err := s.tags.DeleteActorSnapshotTag(ctx, checkpoint.SnapshotAtespace, tagName(checkpoint.GetId())); err != nil && status.Code(err) != codes.NotFound {
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
	checkpoint, err := s.store.GetAgentInstanceCheckpoint(ctx, checkpointID, userID)
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get checkpoint", err)
	}
	if checkpoint.SnapshotContentScope != "DATA" {
		return nil, serviceerrors.NewFailedPrecondition("Checkpoint includes process state and cannot be forked", nil)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to generate AgentInstance identifier", err)
	}
	instance, _, err := s.store.ForkAgentInstance(ctx, checkpointID, userID, requestID, id.String())
	if errors.Is(err, dbpkg.ErrIdempotencyConflict) {
		return nil, serviceerrors.NewAlreadyExists("request_id was already used for a different AgentInstance", err)
	}
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to reserve fork AgentInstance", err)
	}
	instance, err = s.workflow.Fork(ctx, instance, checkpoint)
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
