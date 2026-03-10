package temporal

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

// Client wraps the Temporal SDK client for workflow administration.
type Client struct {
	client    client.Client
	namespace string
}

// NewClient creates a new Temporal client connected to the given host:port.
// It retries with exponential backoff for up to ~60 seconds to handle
// startup ordering (e.g. temporal-ui starting before temporal-server is ready).
func NewClient(hostPort, namespace string) (*Client, error) {
	var c client.Client
	var err error

	backoff := time.Second
	const maxBackoff = 10 * time.Second
	const maxAttempts = 10

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		c, err = client.Dial(client.Options{
			HostPort:  hostPort,
			Namespace: namespace,
		})
		if err == nil {
			return &Client{client: c, namespace: namespace}, nil
		}

		if attempt == maxAttempts {
			break
		}

		log.Printf("failed to connect to Temporal at %s (attempt %d/%d): %v — retrying in %s",
			hostPort, attempt, maxAttempts, err, backoff)
		time.Sleep(backoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	return nil, fmt.Errorf("failed to connect to Temporal at %s after %d attempts: %w", hostPort, maxAttempts, err)
}

// NewClientFromSDK wraps an existing Temporal SDK client (useful for testing).
func NewClientFromSDK(c client.Client, namespace string) *Client {
	return &Client{client: c, namespace: namespace}
}

// Close closes the underlying Temporal connection.
func (c *Client) Close() {
	c.client.Close()
}

// statusToQuery maps user-friendly status strings to Temporal visibility query fragments.
func statusToQuery(status string) string {
	switch strings.ToLower(status) {
	case "running":
		return "ExecutionStatus = 'Running'"
	case "completed":
		return "ExecutionStatus = 'Completed'"
	case "failed":
		return "ExecutionStatus = 'Failed'"
	case "canceled":
		return "ExecutionStatus = 'Canceled'"
	case "terminated":
		return "ExecutionStatus = 'Terminated'"
	case "timed_out", "timedout":
		return "ExecutionStatus = 'TimedOut'"
	default:
		return ""
	}
}

// executionStatusString converts a Temporal workflow execution status enum to a human-readable string.
func executionStatusString(status enumspb.WorkflowExecutionStatus) string {
	switch status {
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:
		return "Running"
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return "Completed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		return "Failed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return "Canceled"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return "Terminated"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return "TimedOut"
	default:
		return "Unknown"
	}
}

// ListWorkflows lists workflow executions matching the given filter.
func (c *Client) ListWorkflows(ctx context.Context, filter WorkflowFilter) ([]*WorkflowSummary, error) {
	var queryParts []string

	if sq := statusToQuery(filter.Status); sq != "" {
		queryParts = append(queryParts, sq)
	}
	if filter.AgentName != "" {
		queryParts = append(queryParts, fmt.Sprintf("WorkflowId STARTS_WITH 'agent-%s-'", filter.AgentName))
	}

	query := strings.Join(queryParts, " AND ")

	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}

	resp, err := c.client.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
		Namespace:     c.namespace,
		Query:         query,
		PageSize:      int32(pageSize),
		NextPageToken: filter.NextToken,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}

	var workflows []*WorkflowSummary
	for _, exec := range resp.Executions {
		agentName, sessionID := ParseWorkflowID(exec.Execution.WorkflowId)
		summary := &WorkflowSummary{
			WorkflowID: exec.Execution.WorkflowId,
			RunID:      exec.Execution.RunId,
			AgentName:  agentName,
			SessionID:  sessionID,
			Status:     executionStatusString(exec.Status),
			StartTime:  exec.StartTime.AsTime(),
			TaskQueue:  exec.TaskQueue,
		}
		if exec.CloseTime != nil && exec.CloseTime.IsValid() {
			ct := exec.CloseTime.AsTime()
			summary.CloseTime = &ct
		}
		workflows = append(workflows, summary)
	}

	return workflows, nil
}

// GetWorkflow retrieves detailed information about a specific workflow execution.
func (c *Client) GetWorkflow(ctx context.Context, workflowID string) (*WorkflowDetail, error) {
	desc, err := c.client.DescribeWorkflowExecution(ctx, workflowID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to describe workflow %s: %w", workflowID, err)
	}

	info := desc.WorkflowExecutionInfo
	agentName, sessionID := ParseWorkflowID(workflowID)

	detail := &WorkflowDetail{
		WorkflowSummary: WorkflowSummary{
			WorkflowID: info.Execution.WorkflowId,
			RunID:      info.Execution.RunId,
			AgentName:  agentName,
			SessionID:  sessionID,
			Status:     executionStatusString(info.Status),
			StartTime:  info.StartTime.AsTime(),
			TaskQueue:  info.TaskQueue,
		},
	}
	if info.CloseTime != nil && info.CloseTime.IsValid() {
		ct := info.CloseTime.AsTime()
		detail.CloseTime = &ct
	}

	// Fetch activity history
	detail.Activities = c.fetchActivities(ctx, workflowID, info.Execution.RunId)

	return detail, nil
}

// fetchActivities extracts activity information from workflow history events.
func (c *Client) fetchActivities(ctx context.Context, workflowID, runID string) []ActivityInfo {
	iter := c.client.GetWorkflowHistory(ctx, workflowID, runID, false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)

	type activityState struct {
		Name      string
		StartTime time.Time
		Attempt   int
	}

	pending := make(map[int64]*activityState) // scheduledEventId -> state
	var activities []ActivityInfo

	for iter.HasNext() {
		event, err := iter.Next()
		if err != nil {
			break
		}

		switch {
		case event.GetActivityTaskScheduledEventAttributes() != nil:
			attrs := event.GetActivityTaskScheduledEventAttributes()
			pending[event.EventId] = &activityState{
				Name: attrs.ActivityType.Name,
			}

		case event.GetActivityTaskStartedEventAttributes() != nil:
			attrs := event.GetActivityTaskStartedEventAttributes()
			if state, ok := pending[attrs.ScheduledEventId]; ok {
				state.StartTime = event.EventTime.AsTime()
				state.Attempt = int(attrs.Attempt)
			}

		case event.GetActivityTaskCompletedEventAttributes() != nil:
			attrs := event.GetActivityTaskCompletedEventAttributes()
			if state, ok := pending[attrs.ScheduledEventId]; ok {
				duration := event.EventTime.AsTime().Sub(state.StartTime)
				activities = append(activities, ActivityInfo{
					Name:      state.Name,
					Status:    "Completed",
					StartTime: state.StartTime,
					Duration:  duration.String(),
					Attempt:   state.Attempt,
				})
				delete(pending, attrs.ScheduledEventId)
			}

		case event.GetActivityTaskFailedEventAttributes() != nil:
			attrs := event.GetActivityTaskFailedEventAttributes()
			if state, ok := pending[attrs.ScheduledEventId]; ok {
				duration := event.EventTime.AsTime().Sub(state.StartTime)
				errMsg := ""
				if attrs.Failure != nil {
					errMsg = attrs.Failure.Message
				}
				activities = append(activities, ActivityInfo{
					Name:      state.Name,
					Status:    "Failed",
					StartTime: state.StartTime,
					Duration:  duration.String(),
					Attempt:   state.Attempt,
					Error:     errMsg,
				})
				delete(pending, attrs.ScheduledEventId)
			}

		case event.GetActivityTaskTimedOutEventAttributes() != nil:
			attrs := event.GetActivityTaskTimedOutEventAttributes()
			if state, ok := pending[attrs.ScheduledEventId]; ok {
				duration := event.EventTime.AsTime().Sub(state.StartTime)
				activities = append(activities, ActivityInfo{
					Name:      state.Name,
					Status:    "TimedOut",
					StartTime: state.StartTime,
					Duration:  duration.String(),
					Attempt:   state.Attempt,
				})
				delete(pending, attrs.ScheduledEventId)
			}

		case event.GetActivityTaskCanceledEventAttributes() != nil:
			attrs := event.GetActivityTaskCanceledEventAttributes()
			if state, ok := pending[attrs.ScheduledEventId]; ok {
				duration := event.EventTime.AsTime().Sub(state.StartTime)
				activities = append(activities, ActivityInfo{
					Name:      state.Name,
					Status:    "Canceled",
					StartTime: state.StartTime,
					Duration:  duration.String(),
					Attempt:   state.Attempt,
				})
				delete(pending, attrs.ScheduledEventId)
			}
		}
	}

	// Add any still-pending (running) activities
	for _, state := range pending {
		if !state.StartTime.IsZero() {
			activities = append(activities, ActivityInfo{
				Name:      state.Name,
				Status:    "Running",
				StartTime: state.StartTime,
				Duration:  time.Since(state.StartTime).Truncate(time.Second).String(),
				Attempt:   state.Attempt,
			})
		}
	}

	if activities == nil {
		activities = []ActivityInfo{}
	}
	return activities
}

// CancelWorkflow cancels a running workflow execution.
func (c *Client) CancelWorkflow(ctx context.Context, workflowID string) error {
	if err := c.client.CancelWorkflow(ctx, workflowID, ""); err != nil {
		return fmt.Errorf("failed to cancel workflow %s: %w", workflowID, err)
	}
	return nil
}

// SignalWorkflow sends a signal to a running workflow execution.
func (c *Client) SignalWorkflow(ctx context.Context, workflowID, signalName string, data interface{}) error {
	if err := c.client.SignalWorkflow(ctx, workflowID, "", signalName, data); err != nil {
		return fmt.Errorf("failed to signal workflow %s: %w", workflowID, err)
	}
	return nil
}
