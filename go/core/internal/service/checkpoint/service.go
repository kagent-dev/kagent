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
	ReserveAgentInstanceCheckpoint(context.Context, *apiv1alpha1.Checkpoint, string, string) (*apiv1alpha1.Checkpoint, error)
	FinalizeAgentInstanceCheckpoint(context.Context, string, string, string) (*apiv1alpha1.Checkpoint, error)
	GetAgentInstanceCheckpoint(context.Context, string, string) (*apiv1alpha1.Checkpoint, error)
	ListAgentInstanceCheckpoints(context.Context, string, string, string, int) ([]*apiv1alpha1.Checkpoint, error)
	GetAgentInstanceCheckpointSnapshot(context.Context, string, string) (*dbpkg.AgentInstanceTaskSnapshot, string, error)
	BeginDeleteAgentInstanceCheckpoint(context.Context, string, string) (*apiv1alpha1.Checkpoint, error)
	DeleteAgentInstanceCheckpoint(context.Context, string, string) error
	ForkAgentInstance(context.Context, string, string, string, string) (*apiv1alpha1.AgentInstance, bool, error)
}

type workflow interface {
	Fork(context.Context, *apiv1alpha1.AgentInstance, *dbpkg.AgentInstanceTaskSnapshot, string) (*apiv1alpha1.AgentInstance, error)
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
	checkpoint, err := s.store.ReserveAgentInstanceCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: id.String(), AgentInstanceId: instanceID}, userID, requestID)
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
		return checkpoint, nil
	}

	snapshot, _, err := s.store.GetAgentInstanceCheckpointSnapshot(ctx, checkpoint.GetId(), userID)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get checkpoint snapshot", err)
	}
	tag, err := s.ensureTag(ctx, checkpoint.GetId(), snapshot)
	if err != nil {
		cleanupErr := s.tags.DeleteActorSnapshotTag(ctx, snapshot.Atespace, tagName(checkpoint.GetId()))
		if cleanupErr == nil || status.Code(cleanupErr) == codes.NotFound {
			_, _ = s.store.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.GetId(), "", err.Error())
		}
		return nil, serviceerrors.NewUnavailable("Failed to retain checkpoint snapshot", err)
	}
	checkpoint, err = s.store.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.GetId(), tag.GetMetadata().GetUid(), "")
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to publish checkpoint", err)
	}
	return checkpoint, nil
}

func (s *Service) ensureTag(ctx context.Context, checkpointID string, reference *dbpkg.AgentInstanceTaskSnapshot) (*ateapipb.ActorSnapshotTag, error) {
	if err := s.verifySnapshot(ctx, reference); err != nil {
		return nil, err
	}
	name := tagName(checkpointID)
	tag, err := s.tags.CreateActorSnapshotTag(ctx, reference.Atespace, name, reference.Name)
	if err != nil {
		tag, err = s.tags.GetActorSnapshotTag(ctx, reference.Atespace, name)
		if err != nil {
			return nil, fmt.Errorf("create snapshot tag: %w", err)
		}
	}
	metadata, snapshot := tag.GetMetadata(), tag.GetSnapshot()
	if metadata.GetAtespace() != reference.Atespace || metadata.GetName() != name || metadata.GetUid() == "" ||
		snapshot.GetAtespace() != reference.Atespace || snapshot.GetName() != reference.Name ||
		tag.GetScope() != ateapipb.ActorSnapshotTagScope_ACTOR_SNAPSHOT_TAG_SCOPE_ATESPACE {
		return nil, fmt.Errorf("snapshot tag %s/%s returned invalid identity", reference.Atespace, name)
	}
	if err := s.verifySnapshot(ctx, reference); err != nil {
		return nil, err
	}
	return tag, nil
}

func (s *Service) verifySnapshot(ctx context.Context, reference *dbpkg.AgentInstanceTaskSnapshot) error {
	snapshot, err := s.tags.GetActorSnapshot(ctx, reference.Atespace, reference.Name)
	if err != nil {
		return fmt.Errorf("get checkpoint snapshot: %w", err)
	}
	metadata := snapshot.GetMetadata()
	if metadata.GetAtespace() != reference.Atespace || metadata.GetName() != reference.Name || metadata.GetUid() != reference.UID {
		return fmt.Errorf("checkpoint snapshot %s/%s identity changed", reference.Atespace, reference.Name)
	}
	if scope := strings.TrimPrefix(snapshot.GetStatus().GetContentScope().String(), "SNAPSHOT_CONTENT_SCOPE_"); scope != reference.ContentScope {
		return fmt.Errorf("checkpoint snapshot %s/%s content scope changed", reference.Atespace, reference.Name)
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
	_, err = s.store.BeginDeleteAgentInstanceCheckpoint(ctx, checkpointID, userID)
	if errors.Is(err, dbpkg.ErrNotFound) {
		return serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return serviceerrors.NewInternal("Failed to begin checkpoint deletion", err)
	}
	snapshot, tagUID, err := s.store.GetAgentInstanceCheckpointSnapshot(ctx, checkpointID, userID)
	if err != nil {
		return serviceerrors.NewInternal("Failed to get checkpoint snapshot", err)
	}
	tag, err := s.tags.GetActorSnapshotTag(ctx, snapshot.Atespace, tagName(checkpointID))
	if err != nil && status.Code(err) != codes.NotFound {
		return serviceerrors.NewUnavailable("Failed to get checkpoint snapshot tag", err)
	}
	if err == nil && (tag.GetMetadata().GetUid() != tagUID ||
		tag.GetSnapshot().GetAtespace() != snapshot.Atespace || tag.GetSnapshot().GetName() != snapshot.Name) {
		return serviceerrors.NewFailedPrecondition("Checkpoint snapshot tag identity changed", nil)
	}
	if err := s.tags.DeleteActorSnapshotTag(ctx, snapshot.Atespace, tagName(checkpointID)); err != nil && status.Code(err) != codes.NotFound {
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
	snapshot, _, err := s.store.GetAgentInstanceCheckpointSnapshot(ctx, checkpointID, userID)
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get checkpoint", err)
	}
	if snapshot.ContentScope != "DATA" {
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
