package grpcserver

import (
	"context"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
)

type sessionServer struct {
	apiv1.UnimplementedSessionServiceServer
	service *sessionsvc.Service
}

func (s *sessionServer) CreateSession(ctx context.Context, request *apiv1.CreateSessionRequest) (*apiv1.CreateSessionResponse, error) {
	session, err := s.service.Create(ctx, request.GetAgent(), request.GetRequestId(), request.GetName())
	if err != nil {
		return nil, err
	}
	return &apiv1.CreateSessionResponse{Session: session}, nil
}

func (s *sessionServer) GetSession(ctx context.Context, request *apiv1.GetSessionRequest) (*apiv1.GetSessionResponse, error) {
	session, err := s.service.Get(ctx, request.GetSessionId())
	if err != nil {
		return nil, err
	}
	return &apiv1.GetSessionResponse{Session: session}, nil
}

func (s *sessionServer) ListSessions(ctx context.Context, request *apiv1.ListSessionsRequest) (*apiv1.ListSessionsResponse, error) {
	result, err := s.service.List(ctx, sessionsvc.ListRequest{
		AllCreators: request.GetAllCreators(),
		Agent:       request.GetAgent(),
		PageSize:    int(request.GetPage().GetLimit()), PageToken: request.GetPage().GetPageToken(),
	})
	if err != nil {
		return nil, err
	}
	return &apiv1.ListSessionsResponse{
		Sessions: result.Sessions,
		Page:     &apiv1.PageResponse{NextPageToken: result.NextPageToken},
	}, nil
}

func (s *sessionServer) UpdateSessionName(ctx context.Context, request *apiv1.UpdateSessionNameRequest) (*apiv1.UpdateSessionNameResponse, error) {
	session, err := s.service.Rename(ctx, request.GetSessionId(), request.GetName())
	if err != nil {
		return nil, err
	}
	return &apiv1.UpdateSessionNameResponse{Session: session}, nil
}

func (s *sessionServer) SuspendSession(ctx context.Context, request *apiv1.SuspendSessionRequest) (*apiv1.SuspendSessionResponse, error) {
	session, err := s.service.Suspend(ctx, request.GetSessionId())
	if err != nil {
		return nil, err
	}
	return &apiv1.SuspendSessionResponse{Session: session}, nil
}

func (s *sessionServer) ResumeSession(ctx context.Context, request *apiv1.ResumeSessionRequest) (*apiv1.ResumeSessionResponse, error) {
	session, err := s.service.Resume(ctx, request.GetSessionId())
	if err != nil {
		return nil, err
	}
	return &apiv1.ResumeSessionResponse{Session: session}, nil
}

func (s *sessionServer) DeleteSession(ctx context.Context, request *apiv1.DeleteSessionRequest) (*apiv1.DeleteSessionResponse, error) {
	session, err := s.service.Delete(ctx, request.GetSessionId())
	if err != nil {
		return nil, err
	}
	return &apiv1.DeleteSessionResponse{Session: session}, nil
}

func (s *sessionServer) CreateSessionShare(ctx context.Context, request *apiv1.CreateSessionShareRequest) (*apiv1.CreateSessionShareResponse, error) {
	share, token, err := s.service.CreateShare(ctx, request.GetSessionId(), request.GetTtl().AsDuration())
	if err != nil {
		return nil, err
	}
	return &apiv1.CreateSessionShareResponse{Share: share, Token: token}, nil
}

func (s *sessionServer) ListSessionShares(ctx context.Context, request *apiv1.ListSessionSharesRequest) (*apiv1.ListSessionSharesResponse, error) {
	result, err := s.service.ListShares(ctx, request.GetSessionId(), int(request.GetPage().GetLimit()), request.GetPage().GetPageToken())
	if err != nil {
		return nil, err
	}
	return &apiv1.ListSessionSharesResponse{
		Shares: result.Shares, Page: &apiv1.PageResponse{NextPageToken: result.NextPageToken},
	}, nil
}

func (s *sessionServer) RevokeSessionShare(ctx context.Context, request *apiv1.RevokeSessionShareRequest) (*apiv1.RevokeSessionShareResponse, error) {
	if err := s.service.RevokeShare(ctx, request.GetShareId()); err != nil {
		return nil, err
	}
	return &apiv1.RevokeSessionShareResponse{}, nil
}
