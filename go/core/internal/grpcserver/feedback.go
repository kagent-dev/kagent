package grpcserver

import (
	"context"

	"github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	feedbackservice "github.com/kagent-dev/kagent/go/core/internal/service/feedback"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type feedbackServer struct {
	apiv1alpha1.UnimplementedFeedbackServiceServer
	service *feedbackservice.Service
}

func newFeedbackServer(service *feedbackservice.Service) *feedbackServer {
	return &feedbackServer{service: service}
}

func (s *feedbackServer) CreateFeedback(ctx context.Context, request *apiv1alpha1.CreateFeedbackRequest) (*apiv1alpha1.CreateFeedbackResponse, error) {
	var issueType *database.FeedbackIssueType
	if request.IssueType != nil {
		issueType = new(database.FeedbackIssueType(*request.IssueType))
	}
	if err := s.service.Create(ctx, feedbackservice.CreateRequest{
		MessageID:    request.MessageId,
		IsPositive:   request.GetIsPositive(),
		FeedbackText: request.GetFeedbackText(),
		IssueType:    issueType,
	}); err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateFeedbackResponse{}, nil
}

func (s *feedbackServer) ListFeedback(ctx context.Context, _ *apiv1alpha1.ListFeedbackRequest) (*apiv1alpha1.ListFeedbackResponse, error) {
	result, err := s.service.List(ctx)
	if err != nil {
		return nil, err
	}
	feedback := make([]*apiv1alpha1.Feedback, 0, len(result))
	for index := range result {
		feedback = append(feedback, feedbackToProto(&result[index]))
	}
	return &apiv1alpha1.ListFeedbackResponse{Feedback: feedback}, nil
}

func feedbackToProto(value *database.Feedback) *apiv1alpha1.Feedback {
	result := &apiv1alpha1.Feedback{
		Id:           value.ID,
		UserId:       value.UserID,
		MessageId:    value.MessageID,
		IsPositive:   value.IsPositive,
		FeedbackText: value.FeedbackText,
	}
	if value.CreatedAt != nil {
		result.CreatedAt = timestamppb.New(*value.CreatedAt)
	}
	if value.UpdatedAt != nil {
		result.UpdatedAt = timestamppb.New(*value.UpdatedAt)
	}
	if value.DeletedAt != nil {
		result.DeletedAt = timestamppb.New(*value.DeletedAt)
	}
	if value.IssueType != nil {
		result.IssueType = new(string(*value.IssueType))
	}
	return result
}
