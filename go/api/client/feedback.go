package client

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
)

// Feedback defines the feedback operations
type Feedback interface {
	CreateFeedback(ctx context.Context, feedback *api.Feedback, userID string) error
	ListFeedback(ctx context.Context, userID string) (*api.StandardResponse[[]api.Feedback], error)
}

// feedbackClient handles feedback-related requests
type feedbackClient struct {
	client *BaseClient
}

// NewFeedbackClient creates a new feedback client
func NewFeedbackClient(client *BaseClient) Feedback {
	return &feedbackClient{client: client}
}

// CreateFeedback creates new feedback
func (c *feedbackClient) CreateFeedback(ctx context.Context, feedback *api.Feedback, userID string) error {
	if feedback == nil {
		return fmt.Errorf("feedback is required")
	}
	userID = c.client.GetUserIDOrDefault(userID)
	feedback.UserID = userID

	client, err := c.client.feedbackServiceClient()
	if err != nil {
		return err
	}
	callContext, cancel := c.client.grpcCallContextForUser(ctx, userID)
	defer cancel()
	request := &apiv1alpha1.CreateFeedbackRequest{
		MessageId:    feedback.MessageID,
		IsPositive:   feedback.IsPositive,
		FeedbackText: feedback.FeedbackText,
	}
	if feedback.IssueType != nil {
		request.IssueType = new(string(*feedback.IssueType))
	}
	_, err = client.CreateFeedback(callContext, request)
	if err != nil {
		return err
	}
	return nil
}

// ListFeedback lists all feedback for a user
func (c *feedbackClient) ListFeedback(ctx context.Context, userID string) (*api.StandardResponse[[]api.Feedback], error) {
	userID = c.client.GetUserIDOrDefault(userID)
	if userID == "" {
		return nil, fmt.Errorf("userID is required")
	}

	client, err := c.client.feedbackServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContextForUser(ctx, userID)
	defer cancel()
	response, err := client.ListFeedback(callContext, &apiv1alpha1.ListFeedbackRequest{})
	if err != nil {
		return nil, err
	}

	feedback := make([]api.Feedback, 0, len(response.GetFeedback()))
	for _, value := range response.GetFeedback() {
		converted := api.Feedback{
			ID:           value.GetId(),
			UserID:       value.GetUserId(),
			MessageID:    value.MessageId,
			IsPositive:   value.GetIsPositive(),
			FeedbackText: value.GetFeedbackText(),
		}
		if value.GetCreatedAt() != nil {
			converted.CreatedAt = new(value.GetCreatedAt().AsTime())
		}
		if value.GetUpdatedAt() != nil {
			converted.UpdatedAt = new(value.GetUpdatedAt().AsTime())
		}
		if value.GetDeletedAt() != nil {
			converted.DeletedAt = new(value.GetDeletedAt().AsTime())
		}
		if value.IssueType != nil {
			converted.IssueType = new(database.FeedbackIssueType(*value.IssueType))
		}
		feedback = append(feedback, converted)
	}
	result := api.NewResponse(feedback, "Successfully listed feedback", false)
	return &result, nil
}

func (c *BaseClient) feedbackServiceClient() (apiv1alpha1.FeedbackServiceClient, error) {
	connection, err := c.grpcConnection()
	if err != nil {
		return nil, err
	}
	return apiv1alpha1.NewFeedbackServiceClient(connection), nil
}
