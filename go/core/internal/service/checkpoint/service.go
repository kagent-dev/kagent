package checkpoint

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

const (
	defaultPageSize          = 50
	maxPageSize              = 100
	checkpointFailureMessage = "Checkpoint snapshot copy could not be verified"
)

type store interface {
	GetCheckpointOperation(context.Context, string, string) (*database.CheckpointOperation, error)
	ListPendingCheckpoints(context.Context, string, int) ([]*database.CheckpointOperation, error)
	ClaimCheckpointOperation(context.Context, string, string, uuid.UUID) (*database.CheckpointOperation, error)
	ReleaseCheckpointOperation(context.Context, string, uuid.UUID) error
	ReserveSessionCheckpoint(context.Context, *apiv1.Checkpoint, string, string, uuid.UUID) (*apiv1.Checkpoint, *database.SessionTaskSnapshot, error)
	FinalizeClaimedSessionCheckpoint(context.Context, *apiv1.Checkpoint, uuid.UUID, string, string, string) (*apiv1.Checkpoint, error)
	GetSessionCheckpoint(context.Context, string, string) (*apiv1.Checkpoint, error)
	GetSessionCheckpointForkSource(context.Context, string, string) (*database.SessionCheckpointForkSource, error)
	ListSessionCheckpoints(context.Context, string, string, string, int) ([]*apiv1.Checkpoint, error)
	BeginDeleteSessionCheckpoint(context.Context, string, string, uuid.UUID) (*database.CheckpointOperation, error)
	DeleteClaimedSessionCheckpoint(context.Context, string, string, uuid.UUID) error
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

var _ store = (*database.Client)(nil)
var _ tagClient = (*substrate.Client)(nil)

type Service struct {
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

// Create executes new work in the request. The reservation owns its bounded
// claim from INSERT, so the normal path needs no subsequent acquisition query.
func (s *Service) Create(ctx context.Context, sessionID, requestID, expectedHeadTaskID string) (result *apiv1.Checkpoint, err error) {
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	executor := uuid.New()
	id, err := uuid.NewV7()
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to generate checkpoint identifier", err)
	}
	op, err := s.reserveCreate(ctx, sessionID, requestID, expectedHeadTaskID, id.String(), executor)
	if err != nil {
		return nil, err
	}
	if op.Checkpoint.State != apiv1.CheckpointState_CHECKPOINT_STATE_CREATING {
		return createOutcome(op.Checkpoint)
	}
	if op.Checkpoint.Id != id.String() {
		result, err = s.claimAndExecute(ctx, op)
	} else {
		result, err = s.execute(ctx, op, executor)
	}
	if err == nil && result == nil {
		return nil, serviceerrors.NewNotFound("Checkpoint no longer exists", nil)
	}
	return result, err
}

func (s *Service) reserveCreate(ctx context.Context, sessionID, requestID, expectedHeadTaskID, id string, executor uuid.UUID) (*database.CheckpointOperation, error) {
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
	checkpoint, snapshot, err := s.store.ReserveSessionCheckpoint(ctx, &apiv1.Checkpoint{Id: id, SessionId: sessionID, HeadTaskId: expectedHeadTaskID}, userID, requestID, executor)
	if err != nil {
		// The INSERT may have committed before its reply was lost.
		err = errors.Join(err, s.release(ctx, id, executor))
	}
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
	return &database.CheckpointOperation{Checkpoint: checkpoint, Snapshot: snapshot, Creator: userID}, nil
}

var errTagCopyFailed = errors.New("snapshot tag copy cannot be published")

// ensureTag synchronizes with any prior backend copy and verifies both sides
// of the copy because ate-api also permits Actors to change outside kagent.
// The CREATING reservation keeps source admission closed until finalization.

func (s *Service) ensureTag(ctx context.Context, checkpoint *apiv1.Checkpoint, reference *database.SessionTaskSnapshot) (*ateapipb.Tag, error) {
	actorName := substrate.ActorName(checkpoint.SessionId)
	actor, beforeErr := s.verifySnapshot(ctx, actorName, reference)
	if beforeErr != nil && status.Code(beforeErr) != codes.FailedPrecondition && status.Code(beforeErr) != codes.NotFound {
		return nil, beforeErr
	}
	name := tagName(checkpoint.Id)
	// v1.0.0 takes Actor and Tag leases before its workflow checks. Interpreting
	// terminal replies requires the backend guarantee that earlier external
	// writes cannot outlive serialization, including cancellation/lease loss;
	// see the architecture document's required backend contract. GetTag alone
	// cannot establish that a pending copy stopped.
	tag, err := s.tags.CreateTag(ctx, reference.Atespace, name, actorName)
	if err != nil {
		switch status.Code(err) {
		case codes.AlreadyExists, codes.FailedPrecondition, codes.NotFound:
			createErr := err
			tag, err = s.tags.GetTag(ctx, reference.Atespace, name)
			if status.Code(err) == codes.NotFound && status.Code(createErr) != codes.AlreadyExists {
				return nil, errors.Join(errTagCopyFailed, createErr)
			}
			if err != nil {
				return nil, fmt.Errorf("read snapshot tag after create: %w", err)
			}
		default:
			// A canceled or unavailable RPC may still own the copy. Keep its
			// reservation and retry through the same backend serialization barrier.
			return nil, fmt.Errorf("create snapshot tag: %w", err)
		}
	}
	if beforeErr != nil {
		return tag, errors.Join(errTagCopyFailed, beforeErr)
	}
	snapshot := tag.GetStatus().GetSnapshot()
	if !validTagSource(tag, checkpoint, reference) ||
		tag.GetStatus().GetActorTemplateUid() != substrate.LatestActorSnapshot(actor).GetActorTemplateUid() ||
		substrate.SnapshotURI(snapshot) == "" || snapshot.GetFidelity().String() != reference.Fidelity {
		return tag, fmt.Errorf("snapshot tag returned invalid identity or incomplete copy: %w", errTagCopyFailed)
	}
	verified, err := s.verifySnapshot(ctx, actorName, reference)
	if err != nil {
		if status.Code(err) == codes.FailedPrecondition || status.Code(err) == codes.NotFound {
			return tag, errors.Join(errTagCopyFailed, err)
		}
		return nil, err
	}
	if verified.GetMetadata().GetUid() != actor.GetMetadata().GetUid() {
		return tag, fmt.Errorf("checkpoint Actor changed while copying: %w", errTagCopyFailed)
	}
	return tag, nil
}

func validTagSource(tag *ateapipb.Tag, checkpoint *apiv1.Checkpoint, snapshot *database.SessionTaskSnapshot) bool {
	metadata, source := tag.GetMetadata(), tag.GetSourceActor()
	return metadata.GetAtespace() == snapshot.Atespace && metadata.GetName() == tagName(checkpoint.Id) && metadata.GetUid() != "" &&
		source.GetAtespace() == snapshot.Atespace && source.GetName() == substrate.ActorName(checkpoint.SessionId) &&
		tag.GetScope() == ateapipb.TagScope_TAG_SCOPE_ATESPACE
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
		return nil, status.Errorf(codes.FailedPrecondition, "checkpoint Actor %s/%s snapshot changed", reference.Atespace, actorName)
	}
	if fidelity := snapshot.GetFidelity().String(); fidelity != reference.Fidelity {
		return nil, status.Errorf(codes.FailedPrecondition, "checkpoint Actor %s/%s snapshot fidelity changed", reference.Atespace, actorName)
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
	return publicCheckpoint(checkpoint), nil
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
	for i, checkpoint := range result.Checkpoints {
		result.Checkpoints[i] = publicCheckpoint(checkpoint)
	}
	if len(rows) > pageSize {
		result.NextPageToken = encodePageToken(rows[pageSize-1].GetId())
	}
	return result, nil
}

func (s *Service) Delete(ctx context.Context, checkpointID string) (err error) {
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	checkpointID, err = validateIdentity(checkpointID)
	if err != nil {
		return err
	}
	userID, err := s.authorize(ctx, auth.VerbDelete, auth.Resource{Type: "Checkpoint", Name: checkpointID})
	if err != nil {
		return err
	}
	executor := uuid.New()
	op, err := s.store.BeginDeleteSessionCheckpoint(ctx, checkpointID, userID, executor)
	if err != nil {
		err = errors.Join(err, s.release(ctx, checkpointID, executor))
		if errors.Is(err, database.ErrConflict) {
			_, err = s.claimAndExecute(ctx, &database.CheckpointOperation{Checkpoint: &apiv1.Checkpoint{Id: checkpointID}, Creator: userID})
			return err
		}
		if errors.Is(err, database.ErrNotFound) {
			return serviceerrors.NewNotFound("Checkpoint not found", err)
		}
		return serviceerrors.NewInternal("Failed to begin checkpoint deletion", err)
	}
	_, err = s.execute(ctx, op, executor)
	return err
}

func (s *Service) finishDelete(ctx context.Context, op *database.CheckpointOperation, executor uuid.UUID) error {
	checkpointID, userID := op.Checkpoint.Id, op.Creator
	snapshot, tagUID := op.Snapshot, op.TagUID
	tag, err := s.tags.GetTag(ctx, snapshot.Atespace, tagName(checkpointID))
	if err != nil && status.Code(err) != codes.NotFound {
		return publicOperationError(fmt.Errorf("get checkpoint snapshot tag: %w", err))
	}
	if err == nil {
		// The retained UID is authoritative for failed partial copies too:
		// their storage may still be PENDING and has no completed snapshot URI.
		if tagUID == "" || tag.GetMetadata().GetUid() != tagUID ||
			tag.GetMetadata().GetAtespace() != snapshot.Atespace || tag.GetMetadata().GetName() != tagName(checkpointID) ||
			(op.Checkpoint.Failure == nil && substrate.SnapshotURI(tag.GetStatus().GetSnapshot()) != snapshot.URI) {
			return serviceerrors.NewFailedPrecondition("Checkpoint snapshot tag identity changed", nil)
		}
		if err := s.tags.DeleteTag(ctx, snapshot.Atespace, tagName(checkpointID), tagUID); err != nil && status.Code(err) != codes.NotFound {
			return publicOperationError(fmt.Errorf("delete checkpoint snapshot tag: %w", err))
		}
	}
	if err := s.store.DeleteClaimedSessionCheckpoint(ctx, checkpointID, userID, executor); err != nil {
		if errors.Is(err, database.ErrConflict) {
			_, readErr := s.store.GetCheckpointOperation(ctx, checkpointID, userID)
			if errors.Is(readErr, database.ErrNotFound) {
				return nil
			}
			if readErr != nil {
				return serviceerrors.NewInternal("Failed to observe checkpoint deletion", readErr)
			}
			return serviceerrors.NewAborted("Checkpoint deletion is still pending; retry the same request", nil)
		}
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

func createOutcome(checkpoint *apiv1.Checkpoint) (*apiv1.Checkpoint, error) {
	switch checkpoint.State {
	case apiv1.CheckpointState_CHECKPOINT_STATE_READY:
		return checkpoint, nil
	case apiv1.CheckpointState_CHECKPOINT_STATE_FAILED:
		failure, err := status.New(codes.FailedPrecondition,
			"Checkpoint creation failed; delete the failed checkpoint before creating a new request").WithDetails(
			&errdetails.ErrorInfo{Domain: "kagent.dev", Reason: "KAGENT_CHECKPOINT_FAILED"},
			&errdetails.ResourceInfo{ResourceType: "kagent.api.v1.Checkpoint", ResourceName: checkpoint.Id},
		)
		if err != nil {
			return nil, serviceerrors.NewInternal("Failed to report checkpoint failure", err)
		}
		return nil, failure.Err()
	default:
		return nil, serviceerrors.NewAborted("Checkpoint operation is still pending; retry the same request", nil)
	}
}

// Failed records from older controllers can contain raw backend errors. Public
// reads expose a generic failure without rewriting the retained operation data.
func publicCheckpoint(checkpoint *apiv1.Checkpoint) *apiv1.Checkpoint {
	if checkpoint.State != apiv1.CheckpointState_CHECKPOINT_STATE_FAILED {
		return checkpoint
	}
	result := proto.CloneOf(checkpoint)
	result.Failure = &apiv1.Failure{Reason: "SnapshotTagFailed", Message: checkpointFailureMessage}
	return result
}

// Backend status text/details can contain private storage configuration. Keep
// current status codes while exposing only a service-owned public message.
func publicOperationError(err error) error {
	if err == nil || serviceerrors.CodeOf(err) != "" || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if backend, ok := status.FromError(err); ok {
		return status.Error(backend.Code(), "Checkpoint operation could not complete")
	}
	return serviceerrors.NewInternal("Checkpoint operation could not complete", err)
}
