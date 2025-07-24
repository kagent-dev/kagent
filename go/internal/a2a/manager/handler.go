// Tencent is pleased to support the open source community by making trpc-a2a-go available.
//
// Copyright (C) 2025 THL A29 Limited, a Tencent company.  All rights reserved.
//
// trpc-a2a-go is licensed under the Apache License Version 2.0.

package manager

import (
	"context"
	"fmt"
	"time"

	"trpc.group/trpc-go/trpc-a2a-go/log"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

// =============================================================================
// MessageHandle Implementation
// =============================================================================

// taskHandler implements TaskHandler interface
type taskHandler struct {
	manager   *TaskManager
	messageID string
	ctx       context.Context
}

var _ taskmanager.TaskHandler = (*taskHandler)(nil)

// UpdateTaskState updates task state
func (h *taskHandler) UpdateTaskState(
	taskID *string,
	state protocol.TaskState,
	message *protocol.Message,
) error {
	if taskID == nil || *taskID == "" {
		return fmt.Errorf("taskID cannot be nil or empty")
	}

	task, err := h.manager.Storage.GetTask(*taskID)
	if err != nil {
		log.Warnf("UpdateTaskState called for non-existent task %s", *taskID)
		return fmt.Errorf("task not found: %s", *taskID)
	}

	originalTask := task.Task()
	originalTask.Status = protocol.TaskStatus{
		State:     state,
		Message:   message,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// Update task in storage
	if err := h.manager.Storage.StoreTask(*taskID, task); err != nil {
		return fmt.Errorf("failed to update task: %w", err)
	}

	log.Debugf("Updated task %s state to %s", *taskID, state)

	// notify subscribers
	finalState := isFinalState(state)
	event := &protocol.TaskStatusUpdateEvent{
		TaskID:    *taskID,
		ContextID: originalTask.ContextID,
		Status:    originalTask.Status,
		Kind:      protocol.KindTaskStatusUpdate,
		Final:     finalState,
	}
	streamEvent := protocol.StreamingMessageEvent{Result: event}
	h.manager.notifySubscribers(*taskID, streamEvent)
	return nil
}

// SubscribeTask subscribes to the task
func (h *taskHandler) SubscribeTask(taskID *string) (taskmanager.TaskSubscriber, error) {
	if taskID == nil || *taskID == "" {
		return nil, fmt.Errorf("taskID cannot be nil or empty")
	}
	if !h.manager.Storage.TaskExists(*taskID) {
		return nil, fmt.Errorf("task not found: %s", *taskID)
	}
	subscriber := NewTaskSubscriber(*taskID, defaultTaskSubscriberBufferSize)
	h.manager.addSubscriber(*taskID, subscriber)
	return subscriber, nil
}

// AddArtifact adds artifact to specified task
func (h *taskHandler) AddArtifact(
	taskID *string,
	artifact protocol.Artifact,
	isFinal bool,
	needMoreData bool,
) error {
	if taskID == nil || *taskID == "" {
		return fmt.Errorf("taskID cannot be nil or empty")
	}

	task, err := h.manager.Storage.GetTask(*taskID)
	if err != nil {
		return fmt.Errorf("task not found: %s", *taskID)
	}

	task.Task().Artifacts = append(task.Task().Artifacts, artifact)

	// Update task in storage
	if err := h.manager.Storage.StoreTask(*taskID, task); err != nil {
		return fmt.Errorf("failed to update task: %w", err)
	}

	log.Debugf("Added artifact %s to task %s", artifact.ArtifactID, *taskID)

	// notify subscribers
	event := &protocol.TaskArtifactUpdateEvent{
		TaskID:    *taskID,
		ContextID: task.Task().ContextID,
		Artifact:  artifact,
		Kind:      protocol.KindTaskArtifactUpdate,
		LastChunk: &isFinal,
		Append:    &needMoreData,
	}
	streamEvent := protocol.StreamingMessageEvent{Result: event}
	h.manager.notifySubscribers(*taskID, streamEvent)

	return nil
}

// GetTask gets task
func (h *taskHandler) GetTask(taskID *string) (taskmanager.CancellableTask, error) {
	if taskID == nil || *taskID == "" {
		return nil, fmt.Errorf("taskID cannot be nil or empty")
	}

	task, err := h.manager.getTask(*taskID)
	if err != nil {
		return nil, err
	}

	// return task copy to avoid external modification
	taskCopy := *task.Task()
	if taskCopy.Artifacts != nil {
		taskCopy.Artifacts = make([]protocol.Artifact, len(task.Task().Artifacts))
		copy(taskCopy.Artifacts, task.Task().Artifacts)
	}
	if taskCopy.History != nil {
		taskCopy.History = make([]protocol.Message, len(task.Task().History))
		copy(taskCopy.History, task.Task().History)
	}

	return &MemoryCancellableTask{
		task:       taskCopy,
		cancelFunc: task.cancelFunc,
		ctx:        task.ctx,
	}, nil
}

// GetContextID gets context ID
func (h *taskHandler) GetContextID() string {
	message, err := h.manager.Storage.GetMessage(h.messageID)
	if err == nil && message.ContextID != nil {
		return *message.ContextID
	}
	return ""
}

// GetMessageHistory gets message history
func (h *taskHandler) GetMessageHistory() []protocol.Message {
	message, err := h.manager.Storage.GetMessage(h.messageID)
	if err == nil && message.ContextID != nil {
		return h.manager.getMessageHistory(*message.ContextID)
	}
	return []protocol.Message{}
}

// BuildTask creates a new task and returns task object
func (h *taskHandler) BuildTask(specificTaskID *string, contextID *string) (string, error) {
	// if no taskID provided, generate one
	var actualTaskID string
	if specificTaskID == nil || *specificTaskID == "" {
		actualTaskID = protocol.GenerateTaskID()
	} else {
		actualTaskID = *specificTaskID
	}

	// Check if task already exists to avoid duplicate WithCancel calls
	if _, err := h.manager.Storage.GetTask(actualTaskID); err == nil {
		log.Warnf("Task %s already exists, returning existing task", actualTaskID)
		return "", fmt.Errorf("task already exists: %s", actualTaskID)
	}

	var actualContextID string
	if contextID == nil || *contextID == "" {
		actualContextID = ""
	} else {
		actualContextID = *contextID
	}

	// create new task
	task := protocol.Task{
		ID:        actualTaskID,
		ContextID: actualContextID,
		Kind:      protocol.KindTask,
		Status: protocol.TaskStatus{
			State:     protocol.TaskStateSubmitted,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		Artifacts: make([]protocol.Artifact, 0),
		History:   make([]protocol.Message, 0),
		Metadata:  make(map[string]interface{}),
	}

	cancellableTask := NewCancellableTask(task)

	// store task
	if err := h.manager.Storage.StoreTask(actualTaskID, cancellableTask); err != nil {
		return "", fmt.Errorf("failed to store task: %w", err)
	}

	log.Debugf("Created new task %s with context %s", actualTaskID, actualContextID)

	return actualTaskID, nil
}

// CleanTask cancels and cleans up the task.
func (h *taskHandler) CleanTask(taskID *string) error {
	if taskID == nil || *taskID == "" {
		return fmt.Errorf("taskID cannot be nil or empty")
	}

	task, err := h.manager.Storage.GetTask(*taskID)
	if err != nil {
		return fmt.Errorf("task not found: %s", *taskID)
	}

	// Cancel the task
	task.Cancel()

	// Clean up subscribers
	h.manager.cleanSubscribers(*taskID)

	return nil
}
