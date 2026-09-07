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
	"google.golang.org/protobuf/types/known/timestamppb"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

type store interface {
	ReserveAgentInstanceCheckpoint(context.Context, dbpkg.AgentInstanceCheckpoint) (*dbpkg.AgentInstanceCheckpoint, error)
	FinalizeAgentInstanceCheckpoint(context.Context, string, string, string) (*dbpkg.AgentInstanceCheckpoint, error)
	GetAgentInstanceCheckpoint(context.Context, string, string, string) (*dbpkg.AgentInstanceCheckpoint, error)
	ListAgentInstanceCheckpoints(context.Context, string, string, string, string, int) ([]dbpkg.AgentInstanceCheckpoint, error)
	BeginDeleteAgentInstanceCheckpoint(context.Context, string, string, string) (*dbpkg.AgentInstanceCheckpoint, error)
	DeleteAgentInstanceCheckpoint(context.Context, string, string, string) error
	ForkAgentInstance(context.Context, string, string, string, string, string) (*apiv1alpha1.AgentInstance, bool, error)
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
	Namespace  string
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

func (s *Service) Create(ctx context.Context, namespace, instanceID, requestID string) (*apiv1alpha1.Checkpoint, error) {
	if err := validateCreate(namespace, instanceID, requestID); err != nil {
		return nil, err
	}
	userID, err := s.authorize(ctx, auth.VerbCreate, "AgentInstance", namespace+"/"+instanceID)
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to generate checkpoint identifier", err)
	}
	instanceUUID := uuid.MustParse(instanceID)
	checkpoint, err := s.store.ReserveAgentInstanceCheckpoint(ctx, dbpkg.AgentInstanceCheckpoint{
		ID: id, Namespace: namespace, SourceInstanceID: instanceUUID, UserID: userID,
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
	if checkpoint.State != "CREATING" {
		return checkpointProto(checkpoint), nil
	}

	tag, err := s.ensureTag(ctx, checkpoint)
	if err != nil {
		cleanupErr := s.tags.DeleteActorSnapshotTag(ctx, checkpoint.SnapshotAtespace, tagName(checkpoint.ID.String()))
		if cleanupErr == nil || status.Code(cleanupErr) == codes.NotFound {
			_, _ = s.store.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.ID.String(), "", err.Error())
		}
		return nil, serviceerrors.NewUnavailable("Failed to retain checkpoint snapshot", err)
	}
	checkpoint, err = s.store.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.ID.String(), tag.GetMetadata().GetUid(), "")
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to publish checkpoint", err)
	}
	return checkpointProto(checkpoint), nil
}

func (s *Service) ensureTag(ctx context.Context, checkpoint *dbpkg.AgentInstanceCheckpoint) (*ateapipb.ActorSnapshotTag, error) {
	if err := s.verifySnapshot(ctx, checkpoint); err != nil {
		return nil, err
	}
	name := tagName(checkpoint.ID.String())
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

func (s *Service) Get(ctx context.Context, namespace, checkpointID string) (*apiv1alpha1.Checkpoint, error) {
	if err := validateIdentity(namespace, checkpointID); err != nil {
		return nil, err
	}
	userID, err := s.authorize(ctx, auth.VerbGet, "Checkpoint", namespace+"/"+checkpointID)
	if err != nil {
		return nil, err
	}
	checkpoint, err := s.store.GetAgentInstanceCheckpoint(ctx, namespace, checkpointID, userID)
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get checkpoint", err)
	}
	return checkpointProto(checkpoint), nil
}

func (s *Service) List(ctx context.Context, request ListRequest) (ListResult, error) {
	if err := validateIdentity(request.Namespace, request.InstanceID); err != nil {
		return ListResult{}, err
	}
	userID, err := s.authorize(ctx, auth.VerbGet, "Checkpoint", request.Namespace)
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
	rows, err := s.store.ListAgentInstanceCheckpoints(ctx, request.Namespace, request.InstanceID, userID, afterID, pageSize+1)
	if err != nil {
		return ListResult{}, serviceerrors.NewInternal("Failed to list checkpoints", err)
	}
	result := ListResult{Checkpoints: make([]*apiv1alpha1.Checkpoint, min(len(rows), pageSize))}
	for i := range result.Checkpoints {
		result.Checkpoints[i] = checkpointProto(&rows[i])
	}
	if len(rows) > pageSize {
		result.NextPageToken = encodePageToken(rows[pageSize-1].ID.String())
	}
	return result, nil
}

func (s *Service) Delete(ctx context.Context, namespace, checkpointID string) error {
	if err := validateIdentity(namespace, checkpointID); err != nil {
		return err
	}
	userID, err := s.authorize(ctx, auth.VerbDelete, "Checkpoint", namespace+"/"+checkpointID)
	if err != nil {
		return err
	}
	checkpoint, err := s.store.BeginDeleteAgentInstanceCheckpoint(ctx, namespace, checkpointID, userID)
	if errors.Is(err, dbpkg.ErrNotFound) {
		return serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return serviceerrors.NewInternal("Failed to begin checkpoint deletion", err)
	}
	tag, err := s.tags.GetActorSnapshotTag(ctx, checkpoint.SnapshotAtespace, tagName(checkpoint.ID.String()))
	if err != nil && status.Code(err) != codes.NotFound {
		return serviceerrors.NewUnavailable("Failed to get checkpoint snapshot tag", err)
	}
	if err == nil && (tag.GetMetadata().GetUid() != checkpoint.TagUID ||
		tag.GetSnapshot().GetAtespace() != checkpoint.SnapshotAtespace || tag.GetSnapshot().GetName() != checkpoint.SnapshotName) {
		return serviceerrors.NewFailedPrecondition("Checkpoint snapshot tag identity changed", nil)
	}
	if err := s.tags.DeleteActorSnapshotTag(ctx, checkpoint.SnapshotAtespace, tagName(checkpoint.ID.String())); err != nil && status.Code(err) != codes.NotFound {
		return serviceerrors.NewUnavailable("Failed to delete checkpoint snapshot tag", err)
	}
	if err := s.store.DeleteAgentInstanceCheckpoint(ctx, namespace, checkpointID, userID); err != nil {
		return serviceerrors.NewInternal("Failed to delete checkpoint", err)
	}
	return nil
}

func (s *Service) Fork(ctx context.Context, namespace, checkpointID, requestID string) (*apiv1alpha1.AgentInstance, error) {
	if err := validateCreate(namespace, checkpointID, requestID); err != nil {
		return nil, err
	}
	userID, err := s.authorize(ctx, auth.VerbCreate, "AgentInstance", namespace)
	if err != nil {
		return nil, err
	}
	checkpoint, err := s.store.GetAgentInstanceCheckpoint(ctx, namespace, checkpointID, userID)
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
	instance, _, err := s.store.ForkAgentInstance(ctx, namespace, checkpointID, userID, requestID, id.String())
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

func checkpointProto(checkpoint *dbpkg.AgentInstanceCheckpoint) *apiv1alpha1.Checkpoint {
	result := &apiv1alpha1.Checkpoint{
		Id: checkpoint.ID.String(), Namespace: checkpoint.Namespace, AgentInstanceId: checkpoint.SourceInstanceID.String(),
		HeadTaskId: checkpoint.HeadTaskID, HistorySequence: uint64(checkpoint.HistorySequence),
		State: checkpointState(checkpoint.State), CreatedAt: timestamppb.New(checkpoint.CreatedAt),
	}
	if checkpoint.Failure != "" {
		result.Failure = &apiv1alpha1.Failure{Reason: "SnapshotTagFailed", Message: checkpoint.Failure}
	}
	return result
}

func checkpointState(state string) apiv1alpha1.CheckpointState {
	switch state {
	case "CREATING":
		return apiv1alpha1.CheckpointState_CHECKPOINT_STATE_CREATING
	case "READY":
		return apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY
	case "FAILED":
		return apiv1alpha1.CheckpointState_CHECKPOINT_STATE_FAILED
	case "DELETING":
		return apiv1alpha1.CheckpointState_CHECKPOINT_STATE_DELETING
	default:
		return apiv1alpha1.CheckpointState_CHECKPOINT_STATE_UNSPECIFIED
	}
}

func validateCreate(namespace, instanceID, requestID string) error {
	if err := validateIdentity(namespace, instanceID); err != nil {
		return err
	}
	if requestID == "" || strings.TrimSpace(requestID) != requestID || len(requestID) > 128 {
		return serviceerrors.NewInvalidArgument("request_id must be 1-128 characters without surrounding whitespace", nil)
	}
	return nil
}

func validateIdentity(namespace, id string) error {
	if problems := utilvalidation.IsDNS1123Label(namespace); len(problems) > 0 {
		return serviceerrors.NewInvalidArgument("namespace is invalid: "+strings.Join(problems, "; "), nil)
	}
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
