package utils

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultShellExecutor(t *testing.T) {
	executor := &DefaultShellExecutor{}

	// Test successful command
	output, err := executor.Exec(context.Background(), "echo", "hello")
	assert.NoError(t, err)
	assert.Equal(t, "hello\n", string(output))

	// Test command with error
	output, err = executor.Exec(context.Background(), "nonexistent-command")
	assert.Error(t, err)
	assert.Empty(t, output)
}

func TestMockShellExecutor(t *testing.T) {
	mock := NewMockShellExecutor()

	t.Run("unmocked command returns error", func(t *testing.T) {
		output, err := mock.Exec(context.Background(), "unmocked", "command")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unmocked command")
		assert.Empty(t, output)
	})

	t.Run("mocked command returns expected result", func(t *testing.T) {
		expectedOutput := "mocked output"
		mock.AddCommandString("kubectl", []string{"get", "pods"}, expectedOutput, nil)

		output, err := mock.Exec(context.Background(), "kubectl", "get", "pods")
		assert.NoError(t, err)
		assert.Equal(t, expectedOutput, string(output))
	})

	t.Run("mocked command with error", func(t *testing.T) {
		expectedError := errors.New("mocked error")
		mock.AddCommandString("helm", []string{"install", "app"}, "", expectedError)

		output, err := mock.Exec(context.Background(), "helm", "install", "app")
		assert.Error(t, err)
		assert.Equal(t, expectedError, err)
		assert.Empty(t, output)
	})

	t.Run("call log tracking", func(t *testing.T) {
		mock.Reset()

		// Execute some commands
		mock.AddCommandString("cmd1", []string{"arg1"}, "output1", nil)
		mock.AddCommandString("cmd2", []string{"arg2", "arg3"}, "output2", nil)

		_, _ = mock.Exec(context.Background(), "cmd1", "arg1")
		_, _ = mock.Exec(context.Background(), "cmd2", "arg2", "arg3")
		_, _ = mock.Exec(context.Background(), "unmocked", "command")

		callLog := mock.GetCallLog()
		require.Len(t, callLog, 3)

		assert.Equal(t, "cmd1", callLog[0].Command)
		assert.Equal(t, []string{"arg1"}, callLog[0].Args)

		assert.Equal(t, "cmd2", callLog[1].Command)
		assert.Equal(t, []string{"arg2", "arg3"}, callLog[1].Args)

		assert.Equal(t, "unmocked", callLog[2].Command)
		assert.Equal(t, []string{"command"}, callLog[2].Args)
	})

	t.Run("reset functionality", func(t *testing.T) {
		// Create a fresh mock for this test
		freshMock := NewMockShellExecutor()
		freshMock.AddCommandString("test", []string{}, "output", nil)
		_, _ = freshMock.Exec(context.Background(), "test")

		assert.Len(t, freshMock.Commands, 1)
		assert.Len(t, freshMock.CallLog, 1)

		freshMock.Reset()

		assert.Len(t, freshMock.Commands, 0)
		assert.Len(t, freshMock.CallLog, 0)
	})
}

func TestContextShellExecutor(t *testing.T) {
	t.Run("default executor when no context value", func(t *testing.T) {
		ctx := context.Background()
		executor := GetShellExecutor(ctx)

		_, ok := executor.(*DefaultShellExecutor)
		assert.True(t, ok, "should return DefaultShellExecutor when no context value")
	})

	t.Run("mock executor from context", func(t *testing.T) {
		mock := NewMockShellExecutor()
		ctx := WithShellExecutor(context.Background(), mock)

		executor := GetShellExecutor(ctx)
		assert.Equal(t, mock, executor, "should return the mock executor from context")
	})

	t.Run("context propagation", func(t *testing.T) {
		mock := NewMockShellExecutor()
		mock.AddCommandString("test", []string{"arg"}, "test output", nil)

		ctx := WithShellExecutor(context.Background(), mock)

		// Test that RunCommandWithContext uses the mock
		output, err := RunCommandWithContext(ctx, "test", []string{"arg"})
		assert.NoError(t, err)
		assert.Equal(t, "test output", output)

		// Verify the command was logged
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "test", callLog[0].Command)
		assert.Equal(t, []string{"arg"}, callLog[0].Args)
	})
}

func TestRunCommandWithMocking(t *testing.T) {
	t.Run("successful command execution with mock", func(t *testing.T) {
		mock := NewMockShellExecutor()
		mock.AddCommandString("kubectl", []string{"get", "pods", "-n", "default"}, "pod1\npod2", nil)

		ctx := WithShellExecutor(context.Background(), mock)

		output, err := RunCommandWithContext(ctx, "kubectl", []string{"get", "pods", "-n", "default"})
		assert.NoError(t, err)
		assert.Equal(t, "pod1\npod2", output)

		// Verify command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "kubectl", callLog[0].Command)
		assert.Equal(t, []string{"get", "pods", "-n", "default"}, callLog[0].Args)
	})

	t.Run("command failure with mock", func(t *testing.T) {
		mock := NewMockShellExecutor()
		expectedError := errors.New("command failed")
		mock.AddCommandString("helm", []string{"install", "app"}, "", expectedError)

		ctx := WithShellExecutor(context.Background(), mock)

		output, err := RunCommandWithContext(ctx, "helm", []string{"install", "app"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "command helm failed")
		assert.Empty(t, output)
	})

	t.Run("multiple commands with mock", func(t *testing.T) {
		mock := NewMockShellExecutor()
		mock.AddCommandString("kubectl", []string{"get", "pods"}, "pod-list", nil)
		mock.AddCommandString("kubectl", []string{"get", "services"}, "service-list", nil)
		mock.AddCommandString("helm", []string{"list"}, "helm-releases", nil)

		ctx := WithShellExecutor(context.Background(), mock)

		// Execute multiple commands
		output1, err1 := RunCommandWithContext(ctx, "kubectl", []string{"get", "pods"})
		assert.NoError(t, err1)
		assert.Equal(t, "pod-list", output1)

		output2, err2 := RunCommandWithContext(ctx, "kubectl", []string{"get", "services"})
		assert.NoError(t, err2)
		assert.Equal(t, "service-list", output2)

		output3, err3 := RunCommandWithContext(ctx, "helm", []string{"list"})
		assert.NoError(t, err3)
		assert.Equal(t, "helm-releases", output3)

		// Verify all commands were logged
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 3)

		assert.Equal(t, "kubectl", callLog[0].Command)
		assert.Equal(t, []string{"get", "pods"}, callLog[0].Args)

		assert.Equal(t, "kubectl", callLog[1].Command)
		assert.Equal(t, []string{"get", "services"}, callLog[1].Args)

		assert.Equal(t, "helm", callLog[2].Command)
		assert.Equal(t, []string{"list"}, callLog[2].Args)
	})
}

func TestShellToolWithMocking(t *testing.T) {
	t.Run("shell tool uses mock executor", func(t *testing.T) {
		mock := NewMockShellExecutor()
		mock.AddCommandString("echo", []string{"hello", "world"}, "hello world", nil)

		ctx := WithShellExecutor(context.Background(), mock)

		params := shellParams{Command: "echo hello world"}
		output, err := shellTool(ctx, params)
		assert.NoError(t, err)
		assert.Equal(t, "hello world", output)

		// Verify command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "echo", callLog[0].Command)
		assert.Equal(t, []string{"hello", "world"}, callLog[0].Args)
	})

	t.Run("shell tool with empty command", func(t *testing.T) {
		mock := NewMockShellExecutor()
		ctx := WithShellExecutor(context.Background(), mock)

		params := shellParams{Command: ""}
		output, err := shellTool(ctx, params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty command")
		assert.Empty(t, output)

		// No commands should be logged
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

func TestMockShellExecutorCommandKey(t *testing.T) {
	mock := NewMockShellExecutor()

	// Test that different argument combinations create different keys
	mock.AddCommandString("kubectl", []string{"get", "pods"}, "pods", nil)
	mock.AddCommandString("kubectl", []string{"get", "services"}, "services", nil)
	mock.AddCommandString("kubectl", []string{}, "kubectl-help", nil)

	// Test first command
	output, err := mock.Exec(context.Background(), "kubectl", "get", "pods")
	assert.NoError(t, err)
	assert.Equal(t, "pods", string(output))

	// Test second command
	output, err = mock.Exec(context.Background(), "kubectl", "get", "services")
	assert.NoError(t, err)
	assert.Equal(t, "services", string(output))

	// Test third command (no args)
	output, err = mock.Exec(context.Background(), "kubectl")
	assert.NoError(t, err)
	assert.Equal(t, "kubectl-help", string(output))
}

// Benchmark tests to ensure mocking doesn't add significant overhead
func BenchmarkDefaultShellExecutor(b *testing.B) {
	executor := &DefaultShellExecutor{}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = executor.Exec(ctx, "echo", "test")
	}
}

func BenchmarkMockShellExecutor(b *testing.B) {
	mock := NewMockShellExecutor()
	mock.AddCommandString("echo", []string{"test"}, "test", nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mock.Exec(ctx, "echo", "test")
	}
}

func BenchmarkRunCommandWithContext(b *testing.B) {
	mock := NewMockShellExecutor()
	mock.AddCommandString("echo", []string{"test"}, "test", nil)
	ctx := WithShellExecutor(context.Background(), mock)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = RunCommandWithContext(ctx, "echo", []string{"test"})
	}
}
