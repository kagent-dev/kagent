package client

import (
	"fmt"
)

func (c *Client) ListSessions(userID string) ([]*Session, error) {
	var sessions []*Session
	err := c.doRequest("GET", fmt.Sprintf("/sessions/?user_id=%s", userID), nil, &sessions)
	return sessions, err
}

func (c *Client) CreateSession(session *CreateSession) (*Session, error) {
	var result Session
	err := c.doRequest("POST", "/sessions/", session, &result)
	return &result, err
}

func (c *Client) GetSessionById(sessionID int, userID string) (*Session, error) {
	var session Session
	err := c.doRequest("GET", fmt.Sprintf("/sessions/%d?user_id=%s", sessionID, userID), nil, &session)
	return &session, err
}

func (c *Client) GetSession(sessionLabel string, userID string) (*Session, error) {
	allSessions, err := c.ListSessions(userID)
	if err != nil {
		return nil, err
	}

	for _, session := range allSessions {
		if session.Name == sessionLabel {
			return session, nil
		}
	}

	return nil, NotFoundError
}

func (c *Client) InvokeSession(sessionID int, userID string, task string) (*TeamResult, error) {
	var result TeamResult
	err := c.doRequest("POST", fmt.Sprintf("/sessions/%d/invoke?user_id=%s", sessionID, userID), struct {
		Task string `json:"task"`
	}{Task: task}, &result)
	return &result, err
}

func (c *Client) InvokeSessionStream(sessionID int, userID string, task string) (<-chan *SseEvent, error) {
	resp, err := c.startRequest("POST", fmt.Sprintf("/sessions/%d/invoke/stream?user_id=%s", sessionID, userID), struct {
		Task string `json:"task"`
	}{Task: task})
	if err != nil {
		return nil, err
	}
	ch := streamSseResponse(resp.Body)
	return ch, nil
}

func (c *Client) DeleteSession(sessionID int, userID string) error {
	return c.doRequest("DELETE", fmt.Sprintf("/sessions/%d?user_id=%s", sessionID, userID), nil, nil)
}

func (c *Client) ListSessionRuns(sessionID int, userID string) ([]*Run, error) {
	var runs SessionRuns
	err := c.doRequest("GET", fmt.Sprintf("/sessions/%d/runs/?user_id=%s", sessionID, userID), nil, &runs)
	if err != nil {
		return nil, err
	}

	var result []*Run
	for _, run := range runs.Runs {
		result = append(result, &run)
	}
	return result, err
}

func (c *Client) UpdateSession(sessionID int, userID string, session *Session) (*Session, error) {
	var updatedSession Session
	err := c.doRequest("PUT", fmt.Sprintf("/sessions/%d?user_id=%s", sessionID, userID), session, &updatedSession)
	return &updatedSession, err
}
