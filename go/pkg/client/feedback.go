package client

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/pkg/client/api"
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
	userID = c.client.GetUserIDOrDefault(userID)
	feedback.UserID = userID

	_, err := c.client.Post(ctx, "/api/feedback", feedback, "")
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

	resp, err := c.client.Get(ctx, "/api/feedback", userID)
	if err != nil {
		return nil, err
	}

	var feedback api.StandardResponse[[]api.Feedback]
	if err := DecodeResponse(resp, &feedback); err != nil {
		return nil, err
	}

	return &feedback, nil
}
