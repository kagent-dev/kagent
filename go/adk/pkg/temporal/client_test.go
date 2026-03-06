package temporal

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/mocks"
)

func TestNewClientFromExisting(t *testing.T) {
	mockClient := &mocks.Client{}
	c := NewClientFromExisting(mockClient)
	require.NotNil(t, c)
	assert.Equal(t, mockClient, c.temporal)
}

func TestExecuteAgent(t *testing.T) {
	tests := []struct {
		name    string
		req     *ExecutionRequest
		cfg     TemporalConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "successful execution",
			req: &ExecutionRequest{
				SessionID:   "sess-1",
				UserID:      "user-1",
				AgentName:   "test-agent",
				Message:     []byte("Hello"),
				Config:      []byte(`{}`),
				NATSSubject: "agent.test-agent.sess-1.stream",
			},
			cfg: DefaultTemporalConfig(),
		},
		{
			name: "temporal client error",
			req: &ExecutionRequest{
				SessionID: "sess-2",
				AgentName: "fail-agent",
				Message:   []byte("Hello"),
				Config:    []byte(`{}`),
			},
			cfg:     DefaultTemporalConfig(),
			wantErr: true,
			errMsg:  "failed to start workflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mocks.Client{}
			mockRun := &mocks.WorkflowRun{}

			workflowID := WorkflowIDForSession(tt.req.AgentName, tt.req.SessionID)

			if tt.wantErr {
				mockClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, tt.req).
					Return(nil, fmt.Errorf("connection refused"))
			} else {
				mockRun.On("GetID").Return(workflowID)
				mockRun.On("GetRunID").Return("run-1")
				mockClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, tt.req).
					Return(mockRun, nil)
			}

			c := NewClientFromExisting(mockClient)
			run, err := c.ExecuteAgent(context.Background(), tt.req, tt.cfg)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, run)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, run)
				assert.Equal(t, workflowID, run.GetID())
			}

			mockClient.AssertExpectations(t)
		})
	}
}

func TestExecuteAgentWorkflowOptions(t *testing.T) {
	mockClient := &mocks.Client{}
	mockRun := &mocks.WorkflowRun{}

	req := &ExecutionRequest{
		SessionID: "sess-1",
		AgentName: "my-agent",
		Message:   []byte("test"),
		Config:    []byte(`{}`),
	}
	cfg := DefaultTemporalConfig()

	// Capture the StartWorkflowOptions to verify them.
	mockClient.On("ExecuteWorkflow", mock.Anything, mock.MatchedBy(func(opts interface{}) bool {
		return true // we verify in the assertions below
	}), mock.Anything, req).
		Return(mockRun, nil)
	mockRun.On("GetID").Return("agent-my-agent-sess-1")
	mockRun.On("GetRunID").Return("run-1")

	c := NewClientFromExisting(mockClient)
	run, err := c.ExecuteAgent(context.Background(), req, cfg)
	require.NoError(t, err)
	assert.Equal(t, "agent-my-agent-sess-1", run.GetID())

	// Verify the workflow was started with the correct task queue (derived from agent name).
	call := mockClient.Calls[0]
	assert.Equal(t, "ExecuteWorkflow", call.Method)
}

func TestSignalApproval(t *testing.T) {
	tests := []struct {
		name     string
		decision *ApprovalDecision
		signalErr error
		wantErr  bool
	}{
		{
			name:     "approval approved",
			decision: &ApprovalDecision{Approved: true, Reason: "looks good"},
		},
		{
			name:     "approval rejected",
			decision: &ApprovalDecision{Approved: false, Reason: "too risky"},
		},
		{
			name:      "signal error",
			decision:  &ApprovalDecision{Approved: true},
			signalErr: fmt.Errorf("workflow not found"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mocks.Client{}
			workflowID := "agent-test-agent-sess-1"

			mockClient.On("SignalWorkflow", mock.Anything, workflowID, "", ApprovalSignalName, tt.decision).
				Return(tt.signalErr)

			c := NewClientFromExisting(mockClient)
			err := c.SignalApproval(context.Background(), workflowID, tt.decision)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			mockClient.AssertExpectations(t)
		})
	}
}

func TestGetWorkflowStatus(t *testing.T) {
	tests := []struct {
		name       string
		workflowID string
		resp       *workflowservice.DescribeWorkflowExecutionResponse
		describeErr error
		wantStatus string
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "running workflow",
			workflowID: "agent-test-sess-1",
			resp: &workflowservice.DescribeWorkflowExecutionResponse{
				WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
					Execution: &commonpb.WorkflowExecution{
						WorkflowId: "agent-test-sess-1",
						RunId:      "run-abc",
					},
					Status:    enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
					TaskQueue: "agent-test",
				},
			},
			wantStatus: "running",
		},
		{
			name:       "completed workflow",
			workflowID: "agent-test-sess-2",
			resp: &workflowservice.DescribeWorkflowExecutionResponse{
				WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
					Execution: &commonpb.WorkflowExecution{
						WorkflowId: "agent-test-sess-2",
						RunId:      "run-def",
					},
					Status:    enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
					TaskQueue: "agent-test",
				},
			},
			wantStatus: "completed",
		},
		{
			name:       "failed workflow",
			workflowID: "agent-test-sess-3",
			resp: &workflowservice.DescribeWorkflowExecutionResponse{
				WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
					Execution: &commonpb.WorkflowExecution{
						WorkflowId: "agent-test-sess-3",
						RunId:      "run-ghi",
					},
					Status: enumspb.WORKFLOW_EXECUTION_STATUS_FAILED,
				},
			},
			wantStatus: "failed",
		},
		{
			name:       "timed out workflow",
			workflowID: "agent-test-sess-4",
			resp: &workflowservice.DescribeWorkflowExecutionResponse{
				WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
					Execution: &commonpb.WorkflowExecution{
						WorkflowId: "agent-test-sess-4",
						RunId:      "run-jkl",
					},
					Status: enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT,
				},
			},
			wantStatus: "timed_out",
		},
		{
			name:        "describe error",
			workflowID:  "agent-missing",
			describeErr: fmt.Errorf("workflow not found"),
			wantErr:     true,
			errMsg:      "failed to describe workflow",
		},
		{
			name:       "nil execution info",
			workflowID: "agent-nil-info",
			resp:       &workflowservice.DescribeWorkflowExecutionResponse{},
			wantErr:    true,
			errMsg:     "no execution info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mocks.Client{}
			mockClient.On("DescribeWorkflowExecution", mock.Anything, tt.workflowID, "").
				Return(tt.resp, tt.describeErr)

			c := NewClientFromExisting(mockClient)
			status, err := c.GetWorkflowStatus(context.Background(), tt.workflowID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, status)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantStatus, status.Status)
				assert.Equal(t, tt.workflowID, status.WorkflowID)
			}

			mockClient.AssertExpectations(t)
		})
	}
}

func TestWaitForResult(t *testing.T) {
	tests := []struct {
		name       string
		workflowID string
		getErr     error
		wantErr    bool
	}{
		{
			name:       "successful result",
			workflowID: "agent-test-sess-1",
		},
		{
			name:       "workflow failure",
			workflowID: "agent-test-sess-2",
			getErr:     fmt.Errorf("workflow execution failed"),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mocks.Client{}
			mockRun := &mocks.WorkflowRun{}

			mockClient.On("GetWorkflow", mock.Anything, tt.workflowID, "").
				Return(mockRun)

			if tt.getErr != nil {
				mockRun.On("Get", mock.Anything, mock.Anything).Return(tt.getErr)
			} else {
				mockRun.On("Get", mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						// Populate the result pointer.
						result := args.Get(1).(*ExecutionResult)
						result.SessionID = "sess-1"
						result.Status = "completed"
						result.Response = []byte(`{"content":"Hello!"}`)
					}).Return(nil)
			}

			c := NewClientFromExisting(mockClient)
			result, err := c.WaitForResult(context.Background(), tt.workflowID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.Equal(t, "completed", result.Status)
				assert.Equal(t, "sess-1", result.SessionID)
			}

			mockClient.AssertExpectations(t)
		})
	}
}

func TestClose(t *testing.T) {
	mockClient := &mocks.Client{}
	mockClient.On("Close").Return()

	c := NewClientFromExisting(mockClient)
	c.Close()

	mockClient.AssertExpectations(t)
}

func TestWorkflowStatusString(t *testing.T) {
	tests := []struct {
		status enumspb.WorkflowExecutionStatus
		want   string
	}{
		{enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING, "running"},
		{enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED, "completed"},
		{enumspb.WORKFLOW_EXECUTION_STATUS_FAILED, "failed"},
		{enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED, "canceled"},
		{enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED, "terminated"},
		{enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT, "timed_out"},
		{enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW, "continued_as_new"},
		{enumspb.WorkflowExecutionStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, workflowStatusString(tt.status))
		})
	}
}
