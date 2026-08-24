package grpcserver

import (
	"context"
	"time"

	"github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	sessionservice "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sessionServer struct {
	apiv1alpha1.UnimplementedSessionServiceServer
	service *sessionservice.Service
}

func newSessionServer(service *sessionservice.Service) *sessionServer {
	return &sessionServer{service: service}
}

func (s *sessionServer) ListSessions(ctx context.Context, _ *apiv1alpha1.ListSessionsRequest) (*apiv1alpha1.ListSessionsResponse, error) {
	values, err := s.service.List(ctx)
	if err != nil {
		return nil, err
	}
	sessions := make([]*apiv1alpha1.Session, 0, len(values))
	for index := range values {
		sessions = append(sessions, sessionToProto(&values[index]))
	}
	return &apiv1alpha1.ListSessionsResponse{Sessions: sessions}, nil
}

func (s *sessionServer) ListSessionsByAgent(ctx context.Context, request *apiv1alpha1.ListSessionsByAgentRequest) (*apiv1alpha1.ListSessionsByAgentResponse, error) {
	ref := request.GetAgentRef()
	values, err := s.service.ListByAgent(ctx, ref.GetNamespace(), ref.GetName())
	if err != nil {
		return nil, err
	}
	sessions := make([]*apiv1alpha1.Session, 0, len(values))
	for index := range values {
		sessions = append(sessions, sessionWithShareToProto(&values[index]))
	}
	return &apiv1alpha1.ListSessionsByAgentResponse{Sessions: sessions}, nil
}

func (s *sessionServer) CreateSession(ctx context.Context, request *apiv1alpha1.CreateSessionRequest) (*apiv1alpha1.CreateSessionResponse, error) {
	source, err := sessionSourceFromProto(request.Source)
	if err != nil {
		return nil, err
	}
	value, err := s.service.Create(ctx, sessionservice.CreateRequest{
		ID:       request.Id,
		AgentRef: request.GetAgentRef(),
		Name:     request.Name,
		Source:   source,
	})
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateSessionResponse{Session: sessionToProto(value)}, nil
}

func (s *sessionServer) GetSession(ctx context.Context, request *apiv1alpha1.GetSessionRequest) (*apiv1alpha1.GetSessionResponse, error) {
	options := database.QueryOptions{OrderAsc: request.GetOrder() == apiv1alpha1.EventOrder_EVENT_ORDER_ASCENDING}
	if request.Limit != nil {
		options.Limit = int(request.GetLimit())
	}
	if request.GetAfter() != nil {
		if err := request.GetAfter().CheckValid(); err != nil {
			return nil, serviceerrors.NewInvalidArgument("after timestamp is invalid", err)
		}
		options.After = request.GetAfter().AsTime()
	}
	result, err := s.service.Get(ctx, request.GetSessionId(), options)
	if err != nil {
		return nil, err
	}
	events := make([]*apiv1alpha1.SessionEvent, 0, len(result.Events))
	for _, value := range result.Events {
		events = append(events, sessionEventToProto(value))
	}
	return &apiv1alpha1.GetSessionResponse{
		Session:  sessionToProto(result.Session),
		Events:   events,
		ReadOnly: result.ReadOnly,
	}, nil
}

func (s *sessionServer) UpdateSession(ctx context.Context, request *apiv1alpha1.UpdateSessionRequest) (*apiv1alpha1.UpdateSessionResponse, error) {
	value, err := s.service.Update(ctx, sessionservice.UpdateRequest{
		SessionID: request.GetSessionId(),
		Name:      request.Name,
		AgentRef:  request.AgentRef,
	})
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.UpdateSessionResponse{Session: sessionToProto(value)}, nil
}

func (s *sessionServer) DeleteSession(ctx context.Context, request *apiv1alpha1.DeleteSessionRequest) (*apiv1alpha1.DeleteSessionResponse, error) {
	if err := s.service.Delete(ctx, request.GetSessionId()); err != nil {
		return nil, err
	}
	return &apiv1alpha1.DeleteSessionResponse{}, nil
}

func (s *sessionServer) AddSessionEvent(ctx context.Context, request *apiv1alpha1.AddSessionEventRequest) (*apiv1alpha1.AddSessionEventResponse, error) {
	if _, err := s.service.AddEvent(ctx, sessionservice.AddEventRequest{
		SessionID: request.GetSessionId(),
		ID:        request.GetId(),
		Data:      request.GetData(),
	}); err != nil {
		return nil, err
	}
	return &apiv1alpha1.AddSessionEventResponse{}, nil
}

func (s *sessionServer) CreateSessionShare(ctx context.Context, request *apiv1alpha1.CreateSessionShareRequest) (*apiv1alpha1.CreateSessionShareResponse, error) {
	value, err := s.service.CreateShare(ctx, request.GetSessionId(), request.ReadOnly)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateSessionShareResponse{Share: sessionShareToProto(value)}, nil
}

func (s *sessionServer) ListSessionShares(ctx context.Context, request *apiv1alpha1.ListSessionSharesRequest) (*apiv1alpha1.ListSessionSharesResponse, error) {
	values, err := s.service.ListShares(ctx, request.GetSessionId())
	if err != nil {
		return nil, err
	}
	shares := make([]*apiv1alpha1.SessionShare, 0, len(values))
	for index := range values {
		shares = append(shares, sessionShareToProto(&values[index]))
	}
	return &apiv1alpha1.ListSessionSharesResponse{Shares: shares}, nil
}

func (s *sessionServer) DeleteSessionShare(ctx context.Context, request *apiv1alpha1.DeleteSessionShareRequest) (*apiv1alpha1.DeleteSessionShareResponse, error) {
	if err := s.service.DeleteShare(ctx, request.GetSessionId(), request.GetToken()); err != nil {
		return nil, err
	}
	return &apiv1alpha1.DeleteSessionShareResponse{}, nil
}

func sessionToProto(value *database.Session) *apiv1alpha1.Session {
	if value == nil {
		return nil
	}
	result := &apiv1alpha1.Session{
		Id:      value.ID,
		Name:    value.Name,
		UserId:  value.UserID,
		AgentId: value.AgentID,
	}
	result.CreatedAt = optionalTimestamp(value.CreatedAt)
	result.UpdatedAt = optionalTimestamp(value.UpdatedAt)
	if value.DeletedAt != nil {
		result.DeletedAt = optionalTimestamp(*value.DeletedAt)
	}
	if value.Source != nil {
		source := sessionSourceToProto(*value.Source)
		result.Source = &source
	}
	return result
}

func sessionWithShareToProto(value *database.SessionWithShareToken) *apiv1alpha1.Session {
	if value == nil {
		return nil
	}
	result := sessionToProto(&value.Session)
	result.ShareToken = value.ShareToken
	result.ShareReadOnly = value.ShareReadOnly
	return result
}

func sessionEventToProto(value *database.Event) *apiv1alpha1.SessionEvent {
	if value == nil {
		return nil
	}
	result := &apiv1alpha1.SessionEvent{
		Id:        value.ID,
		SessionId: value.SessionID,
		UserId:    value.UserID,
		Data:      value.Data,
		CreatedAt: optionalTimestamp(value.CreatedAt),
		UpdatedAt: optionalTimestamp(value.UpdatedAt),
	}
	if value.DeletedAt != nil {
		result.DeletedAt = optionalTimestamp(*value.DeletedAt)
	}
	return result
}

func sessionShareToProto(value *database.SessionShare) *apiv1alpha1.SessionShare {
	if value == nil {
		return nil
	}
	return &apiv1alpha1.SessionShare{
		Id:        value.ID,
		Token:     value.Token,
		SessionId: value.SessionID,
		UserId:    value.UserID,
		ReadOnly:  value.ReadOnly,
		CreatedAt: optionalTimestamp(value.CreatedAt),
	}
}

func optionalTimestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func sessionSourceToProto(value database.SessionSource) apiv1alpha1.SessionSource {
	switch value {
	case database.SessionSourceUser:
		return apiv1alpha1.SessionSource_SESSION_SOURCE_USER
	case database.SessionSourceAgent:
		return apiv1alpha1.SessionSource_SESSION_SOURCE_AGENT
	default:
		return apiv1alpha1.SessionSource_SESSION_SOURCE_UNSPECIFIED
	}
}

func sessionSourceFromProto(value *apiv1alpha1.SessionSource) (*database.SessionSource, error) {
	if value == nil || *value == apiv1alpha1.SessionSource_SESSION_SOURCE_UNSPECIFIED {
		return nil, nil
	}
	var source database.SessionSource
	switch *value {
	case apiv1alpha1.SessionSource_SESSION_SOURCE_USER:
		source = database.SessionSourceUser
	case apiv1alpha1.SessionSource_SESSION_SOURCE_AGENT:
		source = database.SessionSourceAgent
	default:
		return nil, serviceerrors.NewInvalidArgument("source is invalid", nil)
	}
	return &source, nil
}
