package trpcv0

import trpc "trpc.group/trpc-go/trpc-a2a-go/protocol"

// LegacyTextTaskFixture returns a representative trpc-a2a-go task used in migration tests.
func LegacyTextTaskFixture() trpc.Task {
	return trpc.Task{
		ID:        "019d49ab-6830-763c-9db6-1b6359228c4c",
		Kind:      trpc.KindTask,
		ContextID: "ctx-text",
		Status: trpc.TaskStatus{
			State: trpc.TaskStateCompleted,
			Message: &trpc.Message{
				Kind:      trpc.KindMessage,
				MessageID: "msg-status-1",
				Role:      trpc.MessageRoleAgent,
				Parts: []trpc.Part{
					trpc.TextPart{Kind: trpc.KindText, Text: "done"},
				},
			},
		},
		History: []trpc.Message{
			{
				Kind:      trpc.KindMessage,
				MessageID: "msg-user-1",
				Role:      trpc.MessageRoleUser,
				Parts: []trpc.Part{
					trpc.TextPart{Kind: trpc.KindText, Text: "hi"},
				},
			},
		},
		Artifacts: []trpc.Artifact{
			{
				ArtifactID: "artifact-1",
				Parts: []trpc.Part{
					trpc.TextPart{Kind: trpc.KindText, Text: "Hello! How can I assist you with Kubernetes today?"},
				},
			},
		},
	}
}

// LegacyDataTaskFixture returns a trpc-a2a-go task with a data status message.
func LegacyDataTaskFixture() trpc.Task {
	return trpc.Task{
		ID:        "task-data-1",
		Kind:      trpc.KindTask,
		ContextID: "ctx-data",
		Status: trpc.TaskStatus{
			State: trpc.TaskStateInputRequired,
			Message: &trpc.Message{
				Kind:      trpc.KindMessage,
				MessageID: "msg-status-data-1",
				Role:      trpc.MessageRoleAgent,
				Parts: []trpc.Part{
					trpc.DataPart{
						Kind: trpc.KindData,
						Data: map[string]any{
							"name": "adk_request_confirmation",
						},
					},
				},
			},
		},
	}
}

// LegacyPushConfigFixture returns a representative trpc-a2a-go push notification config.
func LegacyPushConfigFixture() trpc.TaskPushNotificationConfig {
	cred := "cred"
	return trpc.TaskPushNotificationConfig{
		TaskID: "task-1",
		PushNotificationConfig: trpc.PushNotificationConfig{
			ID:    "cfg-1",
			URL:   "https://callback.example",
			Token: "tok",
			Authentication: &trpc.AuthenticationInfo{
				Credentials: &cred,
				Schemes:     []string{"Bearer"},
			},
		},
	}
}
