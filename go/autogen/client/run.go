package client

import (
	"fmt"
	"strconv"

	"github.com/google/uuid"
)

func (c *Client) CreateRun(req *CreateRunRequest) (*CreateRunResult, error) {
	var run CreateRunResult
	err := c.doRequest("POST", "/runs", req, &run)
	return &run, err
}

func (c *Client) GetRun(runIDStr string) (*Run, error) {
	// Convert to integer runID
	runID, err := strconv.Atoi(runIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid run ID: %s, must be a number: %w", runIDStr, err)
	}

	var run Run
	err = c.doRequest("GET", fmt.Sprintf("/runs/%d", runID), nil, &run)
	return &run, err
}

func (c *Client) ListRuns(userID string) ([]*Run, error) {
	// Go through all sessions and then retrieve all runs for each session
	var sessions []Session
	err := c.doRequest("GET", fmt.Sprintf("/sessions/?user_id=%s", userID), nil, &sessions)
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

func (c *Client) GetRunMessages(runID uuid.UUID) ([]*RunMessage, error) {
	var messages []*RunMessage
	err := c.doRequest("GET", fmt.Sprintf("/runs/%s/messages", runID), nil, &messages)
	return messages, err
}
