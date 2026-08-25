package agentinstance

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

type store interface {
	CreateAgentInstance(context.Context, *apiv1alpha1.AgentInstance, string) (*apiv1alpha1.AgentInstance, bool, error)
	GetAgentInstance(context.Context, string, string, string) (*apiv1alpha1.AgentInstance, error)
	ListAgentInstances(context.Context, string, string, bool, map[string]string, string, int) ([]*apiv1alpha1.AgentInstance, error)
	CreateAgentInstanceShare(context.Context, dbpkg.AgentInstanceShare) (*dbpkg.AgentInstanceShare, error)
	ListAgentInstanceShares(context.Context, string, string, string, string, int) ([]dbpkg.AgentInstanceShare, error)
	DeleteAgentInstanceShare(context.Context, string, string, string) error
}

type instanceWorkflow interface {
	Create(context.Context, *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error)
	Suspend(context.Context, *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error)
	Resume(context.Context, *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error)
	Delete(context.Context, *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error)
}

type ListRequest struct {
	Namespace   string
	MatchLabels map[string]string
	AllCreators bool
	PageSize    int
	PageToken   string
}

type ListResult struct {
	Instances     []*apiv1alpha1.AgentInstance
	NextPageToken string
}

type ShareListResult struct {
	Shares        []dbpkg.AgentInstanceShare
	NextPageToken string
}

type Service struct {
	store      store
	authorizer auth.Authorizer
	workflow   instanceWorkflow
}

func NewService(store store, authorizer auth.Authorizer, workflow instanceWorkflow) *Service {
	return &Service{store: store, authorizer: authorizer, workflow: workflow}
}

func (s *Service) Create(ctx context.Context, namespace, harness, template, requestID string) (*apiv1alpha1.AgentInstance, error) {
	if err := validateCreate(namespace, harness, template, requestID); err != nil {
		return nil, err
	}
	creator, err := s.authorize(ctx, auth.VerbCreate, namespace+"/"+template)
	if err != nil {
		return nil, err
	}
	instance, _, err := s.store.CreateAgentInstance(ctx, &apiv1alpha1.AgentInstance{
		Id: uuid.NewString(), Namespace: namespace, Creator: creator,
		Harness:       &apiv1alpha1.ResourceReference{Namespace: namespace, Name: harness},
		AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: namespace, Name: template},
	}, requestID)
	if errors.Is(err, dbpkg.ErrIdempotencyConflict) {
		return nil, serviceerrors.NewAlreadyExists("request_id was already used for a different AgentInstance", err)
	}
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, serviceerrors.NewFailedPrecondition("AgentTemplate and Harness do not have a ready prepared revision", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to reserve AgentInstance", err)
	}
	instance, err = s.workflow.Create(ctx, instance)
	if err != nil {
		return nil, serviceerrors.NewUnavailable("Failed to create AgentInstance", err)
	}
	return instance, nil
}

func (s *Service) Get(ctx context.Context, namespace, id string) (*apiv1alpha1.AgentInstance, error) {
	if err := validateIdentity(namespace, id); err != nil {
		return nil, err
	}
	creator, err := s.authorize(ctx, auth.VerbGet, namespace+"/"+id)
	if err != nil {
		return nil, err
	}
	instance, err := s.store.GetAgentInstance(ctx, namespace, id, creator)
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("AgentInstance not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get AgentInstance", err)
	}
	return instance, nil
}

func (s *Service) List(ctx context.Context, request ListRequest) (ListResult, error) {
	if err := validateNamespace(request.Namespace); err != nil {
		return ListResult{}, err
	}
	userID, err := s.authorize(ctx, auth.VerbGet, request.Namespace)
	if err != nil {
		return ListResult{}, err
	}
	if request.AllCreators {
		if _, err := s.authorizeType(ctx, auth.VerbGet, "AgentInstanceAllCreators", request.Namespace); err != nil {
			return ListResult{}, err
		}
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
	instances, err := s.store.ListAgentInstances(ctx, request.Namespace, userID, request.AllCreators, request.MatchLabels, afterID, pageSize+1)
	if err != nil {
		return ListResult{}, serviceerrors.NewInternal("Failed to list AgentInstances", err)
	}
	result := ListResult{Instances: instances}
	if len(result.Instances) > pageSize {
		result.NextPageToken = encodePageToken(result.Instances[pageSize-1].GetId())
		result.Instances = result.Instances[:pageSize]
	}
	return result, nil
}

func (s *Service) Delete(ctx context.Context, namespace, id string) (*apiv1alpha1.AgentInstance, error) {
	if err := validateIdentity(namespace, id); err != nil {
		return nil, err
	}
	creator, err := s.authorize(ctx, auth.VerbDelete, namespace+"/"+id)
	if err != nil {
		return nil, err
	}
	instance, err := s.store.GetAgentInstance(ctx, namespace, id, creator)
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("AgentInstance not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get AgentInstance", err)
	}
	instance, err = s.workflow.Delete(ctx, instance)
	if errors.Is(err, dbpkg.ErrAgentInstanceConflict) {
		return nil, serviceerrors.NewAborted("AgentInstance has a conflicting lifecycle operation", err)
	}
	if err != nil {
		return nil, serviceerrors.NewUnavailable("Failed to delete AgentInstance", err)
	}
	return instance, nil
}

func (s *Service) Suspend(ctx context.Context, namespace, id string) (*apiv1alpha1.AgentInstance, error) {
	if err := validateIdentity(namespace, id); err != nil {
		return nil, err
	}
	creator, err := s.authorize(ctx, auth.VerbUpdate, namespace+"/"+id)
	if err != nil {
		return nil, err
	}
	instance, err := s.store.GetAgentInstance(ctx, namespace, id, creator)
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("AgentInstance not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get AgentInstance", err)
	}
	instance, err = s.workflow.Suspend(ctx, instance)
	if errors.Is(err, dbpkg.ErrAgentInstanceConflict) {
		return nil, serviceerrors.NewAborted("AgentInstance has a conflicting lifecycle operation", err)
	}
	if err != nil {
		return nil, serviceerrors.NewUnavailable("Failed to suspend AgentInstance", err)
	}
	return instance, nil
}

func (s *Service) Resume(ctx context.Context, namespace, id string) (*apiv1alpha1.AgentInstance, error) {
	if err := validateIdentity(namespace, id); err != nil {
		return nil, err
	}
	creator, err := s.authorize(ctx, auth.VerbUpdate, namespace+"/"+id)
	if err != nil {
		return nil, err
	}
	instance, err := s.store.GetAgentInstance(ctx, namespace, id, creator)
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, serviceerrors.NewNotFound("AgentInstance not found", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get AgentInstance", err)
	}
	instance, err = s.workflow.Resume(ctx, instance)
	if errors.Is(err, dbpkg.ErrAgentInstanceConflict) {
		return nil, serviceerrors.NewAborted("AgentInstance has a conflicting lifecycle operation", err)
	}
	if err != nil {
		return nil, serviceerrors.NewUnavailable("Failed to resume AgentInstance", err)
	}
	return instance, nil
}

func (s *Service) CreateShare(ctx context.Context, namespace, instanceID, permission string) (*dbpkg.AgentInstanceShare, string, error) {
	if err := validateIdentity(namespace, instanceID); err != nil {
		return nil, "", err
	}
	if permission != "READ_ONLY" && permission != "READ_WRITE" {
		return nil, "", serviceerrors.NewInvalidArgument("share permission must be READ_ONLY or READ_WRITE", nil)
	}
	creator, err := s.authorize(ctx, auth.VerbCreate, namespace+"/"+instanceID+"/shares")
	if err != nil {
		return nil, "", err
	}
	_, err = s.store.GetAgentInstance(ctx, namespace, instanceID, creator)
	if err != nil {
		if errors.Is(err, dbpkg.ErrNotFound) {
			return nil, "", serviceerrors.NewNotFound("AgentInstance not found", err)
		}
		return nil, "", serviceerrors.NewInternal("Failed to get AgentInstance", err)
	}
	token, tokenHash, err := generateShareToken()
	if err != nil {
		return nil, "", serviceerrors.NewInternal("Failed to create share token", err)
	}
	share, err := s.store.CreateAgentInstanceShare(ctx, dbpkg.AgentInstanceShare{
		ID: uuid.New(), Namespace: namespace, InstanceID: uuid.MustParse(instanceID),
		Creator: creator, Permission: permission, TokenHash: tokenHash,
	})
	if err != nil {
		return nil, "", serviceerrors.NewInternal("Failed to create AgentInstance share", err)
	}
	return share, token, nil
}

func (s *Service) ListShares(ctx context.Context, namespace, instanceID string, pageSize int, pageToken string) (ShareListResult, error) {
	if err := validateIdentity(namespace, instanceID); err != nil {
		return ShareListResult{}, err
	}
	creator, err := s.authorize(ctx, auth.VerbGet, namespace+"/"+instanceID+"/shares")
	if err != nil {
		return ShareListResult{}, err
	}
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if pageSize < 0 || pageSize > maxPageSize {
		return ShareListResult{}, serviceerrors.NewInvalidArgument(fmt.Sprintf("page limit must be between 1 and %d", maxPageSize), nil)
	}
	afterID, err := decodePageToken(pageToken)
	if err != nil {
		return ShareListResult{}, serviceerrors.NewInvalidArgument("page token is invalid", err)
	}
	shares, err := s.store.ListAgentInstanceShares(ctx, namespace, instanceID, creator, afterID, pageSize+1)
	if err != nil {
		return ShareListResult{}, serviceerrors.NewInternal("Failed to list AgentInstance shares", err)
	}
	result := ShareListResult{Shares: shares}
	if len(result.Shares) > pageSize {
		result.NextPageToken = encodePageToken(result.Shares[pageSize-1].ID.String())
		result.Shares = result.Shares[:pageSize]
	}
	return result, nil
}

func (s *Service) RevokeShare(ctx context.Context, namespace, shareID string) error {
	if err := validateIdentity(namespace, shareID); err != nil {
		return err
	}
	creator, err := s.authorize(ctx, auth.VerbDelete, namespace+"/shares/"+shareID)
	if err != nil {
		return err
	}
	if err := s.store.DeleteAgentInstanceShare(ctx, namespace, shareID, creator); err != nil {
		if errors.Is(err, dbpkg.ErrNotFound) {
			return serviceerrors.NewNotFound("AgentInstance share not found", err)
		}
		return serviceerrors.NewInternal("Failed to revoke AgentInstance share", err)
	}
	return nil
}

func (s *Service) authorize(ctx context.Context, verb auth.Verb, name string) (string, error) {
	return s.authorizeType(ctx, verb, "AgentInstance", name)
}

func (s *Service) authorizeType(ctx context.Context, verb auth.Verb, resourceType, name string) (string, error) {
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

func validateCreate(namespace, harness, template, requestID string) error {
	if err := validateNamespace(namespace); err != nil {
		return err
	}
	if problems := utilvalidation.IsDNS1123Subdomain(harness); len(problems) > 0 {
		return serviceerrors.NewInvalidArgument("harness is invalid: "+strings.Join(problems, "; "), nil)
	}
	if problems := utilvalidation.IsDNS1123Subdomain(template); len(problems) > 0 {
		return serviceerrors.NewInvalidArgument("agent_template is invalid: "+strings.Join(problems, "; "), nil)
	}
	if requestID == "" || strings.TrimSpace(requestID) != requestID || len(requestID) > 128 {
		return serviceerrors.NewInvalidArgument("request_id must be 1-128 characters without surrounding whitespace", nil)
	}
	return nil
}

func validateIdentity(namespace, id string) error {
	if err := validateNamespace(namespace); err != nil {
		return err
	}
	if _, err := uuid.Parse(id); err != nil {
		return serviceerrors.NewInvalidArgument("AgentInstance identifier is invalid", err)
	}
	return nil
}

func validateNamespace(namespace string) error {
	if problems := utilvalidation.IsDNS1123Label(namespace); len(problems) > 0 {
		return serviceerrors.NewInvalidArgument("namespace is invalid: "+strings.Join(problems, "; "), nil)
	}
	return nil
}

func encodePageToken(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id))
}

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

func generateShareToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	return token, digest[:], nil
}
