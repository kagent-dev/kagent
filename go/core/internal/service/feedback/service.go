package feedback

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type Service struct {
	store database.Client
}

type CreateRequest struct {
	MessageID    *int64
	IsPositive   bool
	FeedbackText string
	IssueType    *database.FeedbackIssueType
}

func NewService(store database.Client) *Service {
	return &Service{store: store}
}

func (s *Service) Create(ctx context.Context, request CreateRequest) error {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return err
	}
	if request.FeedbackText == "" {
		return serviceerrors.NewInvalidArgument("Missing required field: feedbackText", nil)
	}
	if s.store == nil {
		return serviceerrors.NewInternal("Failed to create feedback", fmt.Errorf("database client is not configured"))
	}

	feedback := &database.Feedback{
		UserID:       userID,
		MessageID:    request.MessageID,
		IsPositive:   request.IsPositive,
		FeedbackText: request.FeedbackText,
		IssueType:    request.IssueType,
	}
	if err := s.store.StoreFeedback(ctx, feedback); err != nil {
		return serviceerrors.NewInternal("Failed to create feedback", err)
	}
	return nil
}

func (s *Service) List(ctx context.Context) ([]database.Feedback, error) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("Failed to list feedback", fmt.Errorf("database client is not configured"))
	}
	feedback, err := s.store.ListFeedback(ctx, userID)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to list feedback", err)
	}
	return feedback, nil
}

func authenticatedUserID(ctx context.Context) (string, error) {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok || session == nil {
		return "", serviceerrors.NewUnauthenticated("Failed to get authenticated principal", fmt.Errorf("no session found"))
	}
	return session.Principal().User.ID, nil
}
