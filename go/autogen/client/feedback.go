package client

import "fmt"

func (c *Client) CreateFeedback(feedback *FeedbackSubmission) error {
	err := c.doRequest("POST", "/feedback/", feedback, nil)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) ListFeedback(userID string) ([]*FeedbackSubmission, error) {
	var response []*FeedbackSubmission
	err := c.doRequest("GET", fmt.Sprintf("/feedback/?user_id=%s", userID), nil, &response)
	if err != nil {
		return nil, err
	}

	return response, nil
}
