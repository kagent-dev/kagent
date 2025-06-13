package client

import (
	"context"
	"fmt"
)

func (c *client) CreateFeedback(feedback *FeedbackSubmission) error {
	err := c.doRequest(context.Background(), "POST", "/feedback/", feedback, nil)
	if err != nil {
		return err
	}

	return nil
}

func (c *client) ListFeedback(userID string) ([]*FeedbackSubmission, error) {
	var response []*FeedbackSubmission
	err := c.doRequest(context.Background(), "GET", fmt.Sprintf("/feedback/?user_id=%s", userID), nil, &response)
	if err != nil {
		return nil, err
	}

	return response, nil
}
