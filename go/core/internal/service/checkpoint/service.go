package checkpoint

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"golang.org/x/sync/singleflight"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

type store interface {
	ReserveSessionCheckpoint(context.Context, *apiv1.Checkpoint, string, string) (*apiv1.Checkpoint, *database.SessionTaskSnapshot, error)
	FinalizeSessionCheckpoint(context.Context, string, string, string, string) (*apiv1.Checkpoint, error)
	GetSessionCheckpoint(context.Context, string, string) (*apiv1.Checkpoint, error)
	GetSessionCheckpointForkSource(context.Context, string, string) (*database.SessionCheckpointForkSource, error)
	ListSessionCheckpoints(context.Context, string, string, string, int) ([]*apiv1.Checkpoint, error)
	BeginDeleteSessionCheckpoint(context.Context, string, string) (*database.SessionTaskSnapshot, string, error)
	DeleteSessionCheckpoint(context.Context, string, string) error
	ForkSession(context.Context, string, string, string, string) (*apiv1.Session, bool, error)
	UpdateCheckpointName(context.Context, string, string, string) (*apiv1.Checkpoint, error)
}

type workflow interface {
	Create(context.Context, *apiv1.Session) (*apiv1.Session, error)
}

type tagClient interface {
	GetActor(context.Context, string, string) (*ateapipb.Actor, error)
	GetTag(context.Context, string, string) (*ateapipb.Tag, error)
	CreateTag(context.Context, string, string, string) (*ateapipb.Tag, error)
	DeleteTag(context.Context, string, string, string) error
}

type Service struct {
	// creates coalesces identical requests within this service session.
	// TODO: route tag creation and cleanup through durable session ownership
	// so retries on different replicas cannot race.
	creates    singleflight.Group
	store      store
	authorizer auth.Authorizer
	tags       tagClient
	workflow   workflow
}

type ListRequest struct {
	SessionID string
	PageSize  int
	PageToken string
}

type ListResult struct {
	Checkpoints   []*apiv1.Checkpoint
	NextPageToken string
}

func NewService(store store, authorizer auth.Authorizer, tags tagClient, workflow workflow) *Service {
	return &Service{store: store, authorizer: authorizer, tags: tags, workflow: workflow}
}

func (s *Service) Create(ctx context.Context, sessionID, requestID, expectedHeadTaskID string) (*apiv1.Checkpoint, error) {
	sessionID, err := validateCreate(sessionID, requestID)
	if err != nil {
		return nil, err
	}
	if expectedHeadTaskID == "" {
		return nil, status.Error(codes.InvalidArgument, "expected_head_task_id is required")
	}
	userID, err := s.authorize(ctx, auth.VerbCreate, auth.Resource{Type: "Checkpoint"})
	if err != nil {
		return nil, err
	}
	if _, err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "Session", Name: sessionID}); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%q/%q/%q/%q", userID, sessionID, requestID, expectedHeadTaskID)
	result, err, _ := s.creates.Do(key, func() (any, error) {
		return s.create(ctx, userID, sessionID, requestID, expectedHeadTaskID)
	})
	if err != nil {
		return nil, err
	}
	return result.(*apiv1.Checkpoint), nil
}

func (s *Service) create(ctx context.Context, userID, sessionID, requestID, expectedHeadTaskID string) (*apiv1.Checkpoint, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to generate checkpoint identifier", err)
	}
	checkpoint, snapshot, err := s.store.ReserveSessionCheckpoint(ctx, &apiv1.Checkpoint{Id: id.String(), SessionId: sessionID, HeadTaskId: expectedHeadTaskID}, userID, requestID)
	if errors.Is(err, database.ErrCheckpointAdvanced) || errors.Is(err, database.ErrSnapshotPending) {
		reason := "KAGENT_CHECKPOINT_CONVERSATION_ADVANCED"
		if errors.Is(err, database.ErrSnapshotPending) {
			reason = "KAGENT_CHECKPOINT_SNAPSHOT_PENDING"
		}
		failure, detailErr := status.New(codes.FailedPrecondition, err.Error()).WithDetails(&errdetails.ErrorInfo{Domain: "kagent.dev", Reason: reason})
		if detailErr != nil {
			return nil, detailErr
		}
		return nil, failure.Err()
	}
	if errors.Is(err, database.ErrIdempotencyConflict) {
		return nil, serviceerrors.NewAlreadyExists("request_id was already used for a different checkpoint", err)
	}
	if errors.Is(err, database.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("Session not found", err)
	}
	if errors.Is(err, database.ErrConflict) || errors.Is(err, database.ErrFailedPrecondition) {
		return nil, serviceerrors.NewFailedPrecondition(err.Error(), err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to reserve checkpoint", err)
	}
	if checkpoint.State != apiv1.CheckpointState_CHECKPOINT_STATE_CREATING {
		return checkpoint, nil
	}

	tag, err := s.ensureTag(ctx, checkpoint, snapshot)
	if err != nil {
		cleanupErr := s.tags.DeleteTag(ctx, snapshot.Atespace, tagName(checkpoint.GetId()), "")
		if cleanupErr == nil || status.Code(cleanupErr) == codes.NotFound {
			_, finalizeErr := s.store.FinalizeSessionCheckpoint(ctx, checkpoint.GetId(), "", "", err.Error())
			err = errors.Join(err, finalizeErr)
		} else {
			err = errors.Join(err, fmt.Errorf("cleanup checkpoint tag: %w", cleanupErr))
		}
		return nil, serviceerrors.NewUnavailable("Failed to retain checkpoint snapshot", err)
	}
	checkpoint, err = s.store.FinalizeSessionCheckpoint(ctx, checkpoint.GetId(), tag.GetMetadata().GetUid(), substrate.SnapshotURI(tag.GetStatus().GetSnapshot()), "")
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to publish checkpoint", err)
	}
	return checkpoint, nil
}

// The CREATING reservation blocks task admission and lifecycle changes while
// CreateTag copies the Actor's current snapshot. Verify both sides of the copy
// because ate-api also permits Actors to be changed outside kagent.
func (s *Service) ensureTag(ctx context.Context, checkpoint *apiv1.Checkpoint, reference *database.SessionTaskSnapshot) (*ateapipb.Tag, error) {
	actorName := substrate.ActorName(checkpoint.GetSessionId())
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
		tag.GetStatus().GetActorTemplateUid() != substrate.LatestActorSnapshot(actor).GetActorTemplateUid() ||
		substrate.SnapshotURI(snapshot) == "" ||
		snapshot.GetFidelity().String() != reference.Fidelity ||
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

func (s *Service) verifySnapshot(ctx context.Context, actorName string, reference *database.SessionTaskSnapshot) (*ateapipb.Actor, error) {
	actor, err := s.tags.GetActor(ctx, reference.Atespace, actorName)
	if err != nil {
		return nil, fmt.Errorf("get checkpoint Actor: %w", err)
	}
	metadata, snapshot := actor.GetMetadata(), substrate.LatestActorSnapshot(actor)
	if metadata.GetAtespace() != reference.Atespace || metadata.GetName() != actorName || metadata.GetUid() == "" ||
		actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED ||
		reference.URI == "" || substrate.SnapshotURI(snapshot) != reference.URI || snapshot.GetActorTemplateUid() == "" {
		return nil, fmt.Errorf("checkpoint Actor %s/%s snapshot changed", reference.Atespace, actorName)
	}
	if fidelity := snapshot.GetFidelity().String(); fidelity != reference.Fidelity {
		return nil, fmt.Errorf("checkpoint Actor %s/%s snapshot fidelity changed", reference.Atespace, actorName)
	}
	return actor, nil
}

func (s *Service) Get(ctx context.Context, checkpointID string) (*apiv1.Checkpoint, error) {
	checkpointID, err := validateIdentity(checkpointID)
	if err != nil {
		return nil, err
	}
	userID, err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "Checkpoint", Name: checkpointID})
	if err != nil {
		return nil, err
	}
	checkpoint, err := s.store.GetSessionCheckpoint(ctx, checkpointID, userID)
	if errors.Is(err, database.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get checkpoint", err)
	}
	return checkpoint, nil
}

func (s *Service) List(ctx context.Context, request ListRequest) (ListResult, error) {
	sessionID, err := validateIdentity(request.SessionID)
	if err != nil {
		return ListResult{}, err
	}
	userID, err := s.authorize(ctx, auth.VerbList, auth.Resource{Type: "Checkpoint"})
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
	rows, err := s.store.ListSessionCheckpoints(ctx, sessionID, userID, afterID, pageSize+1)
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
	checkpointID, err := validateIdentity(checkpointID)
	if err != nil {
		return err
	}
	userID, err := s.authorize(ctx, auth.VerbDelete, auth.Resource{Type: "Checkpoint", Name: checkpointID})
	if err != nil {
		return err
	}
	snapshot, tagUID, err := s.store.BeginDeleteSessionCheckpoint(ctx, checkpointID, userID)
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
	if err == nil {
		// Failed creation records no tag UID after compensation. An unexpected tag
		// at that name is not ours to delete, even if its snapshot URI matches.
		if tagUID == "" || tag.GetMetadata().GetUid() != tagUID ||
			tag.GetMetadata().GetAtespace() != snapshot.Atespace || tag.GetMetadata().GetName() != tagName(checkpointID) ||
			substrate.SnapshotURI(tag.GetStatus().GetSnapshot()) != snapshot.URI {
			return serviceerrors.NewFailedPrecondition("Checkpoint snapshot tag identity changed", nil)
		}
		if err := s.tags.DeleteTag(ctx, snapshot.Atespace, tagName(checkpointID), tagUID); err != nil && status.Code(err) != codes.NotFound {
			return serviceerrors.NewUnavailable("Failed to delete checkpoint snapshot tag", err)
		}
	}
	if err := s.store.DeleteSessionCheckpoint(ctx, checkpointID, userID); err != nil {
		return serviceerrors.NewInternal("Failed to delete checkpoint", err)
	}
	return nil
}

// Rename sets the checkpoint's display name, which forks taken from it inherit. It
// authorizes as a write: reading a checkpoint must not confer retitling it.
func (s *Service) Rename(ctx context.Context, checkpointID, name string) (*apiv1.Checkpoint, error) {
	checkpointID, err := validateIdentity(checkpointID)
	if err != nil {
		return nil, err
	}
	userID, err := s.authorize(ctx, auth.VerbUpdate, auth.Resource{Type: "Checkpoint", Name: checkpointID})
	if err != nil {
		return nil, err
	}
	checkpoint, err := s.store.UpdateCheckpointName(ctx, checkpointID, userID, name)
	if errors.Is(err, database.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to rename checkpoint", err)
	}
	return checkpoint, nil
}

func (s *Service) Fork(ctx context.Context, checkpointID, requestID string) (*apiv1.Session, error) {
	checkpointID, err := validateCreate(checkpointID, requestID)
	if err != nil {
		return nil, err
	}
	userID, err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "Checkpoint", Name: checkpointID})
	if err != nil {
		return nil, err
	}
	source, err := s.store.GetSessionCheckpointForkSource(ctx, checkpointID, userID)
	if errors.Is(err, database.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get checkpoint fork source", err)
	}
	authSession, _ := auth.AuthSessionFrom(ctx) // authorize authenticated the caller.
	// A retained checkpoint outlives its source Session. Authorize its saved
	// source ID and target Agent without requiring a live source Session.
	sourceID, err := validateIdentity(source.Checkpoint.GetSessionId())
	if err != nil {
		return nil, serviceerrors.NewInternal("Invalid checkpoint source Session", err)
	}
	if _, err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "Session", Name: sourceID}); err != nil {
		return nil, err
	}
	agent := source.Agent
	if agent.GetNamespace() == "" || agent.GetName() == "" {
		return nil, serviceerrors.NewFailedPrecondition("Checkpoint has no target Agent", nil)
	}
	if _, err := s.authorize(ctx, auth.VerbCreate, auth.Resource{Type: "Session", Namespace: agent.Namespace}); err != nil {
		return nil, err
	}
	if err := s.authorizer.Check(ctx, authSession.Principal(), auth.VerbGet, auth.Resource{Type: "Agent", Namespace: agent.Namespace, Name: agent.Name}); err != nil {
		return nil, serviceerrors.NewPermissionDenied("Not authorized to access target Agent", err)
	}
	snapshot, tagUID := source.Snapshot, source.TagUID
	if snapshot.Fidelity != "SNAPSHOT_FIDELITY_VOLUMES" {
		return nil, serviceerrors.NewFailedPrecondition("Only volume checkpoints can be forked", nil)
	}
	tag, err := s.tags.GetTag(ctx, snapshot.Atespace, tagName(checkpointID))
	if err != nil {
		return nil, serviceerrors.NewUnavailable("Failed to get checkpoint tag", err)
	}
	if tagUID == "" || tag.GetMetadata().GetUid() != tagUID ||
		tag.GetMetadata().GetAtespace() != snapshot.Atespace || tag.GetMetadata().GetName() != tagName(checkpointID) ||
		snapshot.URI == "" || substrate.SnapshotURI(tag.GetStatus().GetSnapshot()) != snapshot.URI ||
		tag.GetStatus().GetSnapshot().GetFidelity() != ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES {
		return nil, serviceerrors.NewFailedPrecondition("Checkpoint tag identity changed", nil)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to generate Session identifier", err)
	}
	session, _, err := s.store.ForkSession(ctx, checkpointID, userID, requestID, id.String())
	if errors.Is(err, database.ErrIdempotencyConflict) {
		return nil, serviceerrors.NewAlreadyExists("request_id was already used for a different Session", err)
	}
	if errors.Is(err, database.ErrFailedPrecondition) {
		return nil, serviceerrors.NewFailedPrecondition("request_id belongs to a deleted Session", err)
	}
	if errors.Is(err, database.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("Checkpoint not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to reserve fork Session", err)
	}
	session, err = s.workflow.Create(ctx, session)
	if errors.Is(err, database.ErrConflict) {
		return nil, serviceerrors.NewAborted(err.Error(), err)
	}
	if errors.Is(err, database.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("Session was deleted", err)
	}
	if err != nil {
		return nil, serviceerrors.NewUnavailable("Failed to create fork Session", err)
	}
	return session, nil
}

func (s *Service) authorize(ctx context.Context, verb auth.Verb, resource auth.Resource) (string, error) {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return "", serviceerrors.NewUnauthenticated("Failed to get authenticated principal", nil)
	}
	if _, shared := auth.ShareContextFrom(ctx); shared {
		return "", serviceerrors.NewPermissionDenied("Session shares do not grant checkpoint access", nil)
	}
	principal := session.Principal()
	if err := s.authorizer.Check(ctx, principal, verb, resource); err != nil {
		return "", serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	return principal.ID, nil
}

func validateCreate(id, requestID string) (string, error) {
	canonical, err := validateIdentity(id)
	if err != nil {
		return "", err
	}
	if requestID == "" || strings.TrimSpace(requestID) != requestID || len(requestID) > 128 {
		return "", serviceerrors.NewInvalidArgument("request_id must be 1-128 characters without surrounding whitespace", nil)
	}
	return canonical, nil
}

func validateIdentity(id string) (string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return "", serviceerrors.NewInvalidArgument("identifier is invalid", err)
	}
	return parsed.String(), nil
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
	return validateIdentity(string(value))
}
