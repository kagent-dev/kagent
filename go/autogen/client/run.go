package client

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

func (c *client) CreateRun(req *CreateRunRequest) (*CreateRunResult, error) {
	var run CreateRunResult
	err := c.doRequest(context.Background(), "POST", "/runs", req, &run)
	return &run, err
}

func (c *client) GetRun(runID int) (*Run, error) {

	var run Run
	err := c.doRequest(context.Background(), "GET", fmt.Sprintf("/runs/%d", runID), nil, &run)
	return &run, err
}

func (c *client) ListRuns(userID string) ([]*Run, error) {
	// Go through all sessions and then retrieve all runs for each session
	var sessions []Session
	err := c.doRequest(context.Background(), "GET", fmt.Sprintf("/sessions/?user_id=%s", userID), nil, &sessions)
	if err != nil {
		return nil, err
	}

	// For each session, get the run information
	var runs []*Run
	for _, session := range sessions {
		sessionRuns, err := c.ListSessionRuns(session.ID, userID)
		if err != nil {
			return nil, err
		}
		runs = append(runs, sessionRuns...)
	}
	return runs, nil
}

func (c *client) GetRunMessages(runID uuid.UUID) ([]*RunMessage, error) {
	var messages []*RunMessage
	err := c.doRequest(context.Background(), "GET", fmt.Sprintf("/runs/%s/messages", runID), nil, &messages)
	return messages, err
}

func (c *client) DeleteRun(runID uuid.UUID) error {
	return c.doRequest(context.Background(), "DELETE", fmt.Sprintf("/runs/%s", runID), nil, nil)
}
