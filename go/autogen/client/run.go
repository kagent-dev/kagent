package client

import (
	"fmt"

	"github.com/google/uuid"
)

func (c *Client) CreateRun(req *CreateRunRequest) (*CreateRunResult, error) {
	var run CreateRunResult
	err := c.doRequest("POST", "/runs", req, &run)
	return &run, err
}

func (c *Client) GetRun(runID string) (*Run, error) {
	var run Run
	err := c.doRequest("GET", fmt.Sprintf("/runs/%s", runID), nil, &run)
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
		var sessionRuns SessionRuns
		err := c.doRequest("GET", fmt.Sprintf("/sessions/%d/runs/?user_id=%s", session.ID, userID), nil, &sessionRuns)
		if err != nil {
			fmt.Println("Error getting runs for session")
			return nil, err
		}
		for _, run := range sessionRuns.Runs {
			run.Messages, err = c.GetRunMessages(run.ID)
			if err != nil {
				return nil, err
			}
			runs = append(runs, &run)
		}
	}
	return runs, nil
}

func (c *Client) GetRunMessages(runID uuid.UUID) ([]*RunMessage, error) {
	var messages []*RunMessage
	err := c.doRequest("GET", fmt.Sprintf("/runs/%s/messages", runID), nil, &messages)
	return messages, err
}
