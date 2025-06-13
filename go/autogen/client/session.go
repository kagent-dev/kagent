package client

import (
	"context"
	"fmt"
)

func (c *client) ListSessions(userID string) ([]*Session, error) {
	var sessions []*Session
	err := c.doRequest(context.Background(), "GET", fmt.Sprintf("/sessions/?user_id=%s", userID), nil, &sessions)
	return sessions, err
}

func (c *client) CreateSession(session *CreateSession) (*Session, error) {
	var result Session
	err := c.doRequest(context.Background(), "POST", "/sessions/", session, &result)
	return &result, err
}

func (c *client) GetSessionById(sessionID int, userID string) (*Session, error) {
	var session Session
	err := c.doRequest(context.Background(), "GET", fmt.Sprintf("/sessions/%d?user_id=%s", sessionID, userID), nil, &session)
	return &session, err
}

func (c *client) GetSession(sessionLabel string, userID string) (*Session, error) {
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

func (c *client) InvokeSession(sessionID int, userID string, request *InvokeRequest) (*TeamResult, error) {
	var result TeamResult
	err := c.doRequest(context.Background(), "POST", fmt.Sprintf("/sessions/%d/invoke?user_id=%s", sessionID, userID), request, &result)
	return &result, err
}

func (c *client) InvokeSessionStream(sessionID int, userID string, request *InvokeRequest) (<-chan *SseEvent, error) {
	resp, err := c.startRequest(context.Background(), "POST", fmt.Sprintf("/sessions/%d/invoke/stream?user_id=%s", sessionID, userID), request)
	if err != nil {
		return nil, err
	}
	ch := streamSseResponse(resp.Body)
	return ch, nil
}

func (c *client) DeleteSession(sessionID int, userID string) error {
	return c.doRequest(context.Background(), "DELETE", fmt.Sprintf("/sessions/%d?user_id=%s", sessionID, userID), nil, nil)
}

func (c *client) ListSessionRuns(sessionID int, userID string) ([]*Run, error) {
	var runs SessionRuns
	err := c.doRequest(context.Background(), "GET", fmt.Sprintf("/sessions/%d/runs/?user_id=%s", sessionID, userID), nil, &runs)
	if err != nil {
		return nil, err
	}

	var result []*Run
	for _, run := range runs.Runs {
		result = append(result, &run)
	}
	return result, err
}

func (c *client) UpdateSession(sessionID int, userID string, session *Session) (*Session, error) {
	var updatedSession Session
	err := c.doRequest(context.Background(), "PUT", fmt.Sprintf("/sessions/%d?user_id=%s", sessionID, userID), session, &updatedSession)
	return &updatedSession, err
}
