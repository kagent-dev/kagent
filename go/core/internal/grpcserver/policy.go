package grpcserver

import (
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

type MethodPolicies map[string]auth.AccessMode

func DefaultMethodPolicies() MethodPolicies {
	policies := MethodPolicies{
		apiv1alpha1.TaskStoreService_CreateTask_FullMethodName:                auth.AccessRuntime,
		apiv1alpha1.TaskStoreService_GetTask_FullMethodName:                   auth.AccessRuntime,
		apiv1alpha1.TaskStoreService_UpdateTask_FullMethodName:                auth.AccessRuntime,
		apiv1alpha1.TaskStoreService_SettleTask_FullMethodName:                auth.AccessRuntime,
		apiv1alpha1.TaskStoreService_ListTasks_FullMethodName:                 auth.AccessRuntime,
		apiv1alpha1.SystemService_GetVersion_FullMethodName:                   auth.AccessPublic,
		apiv1alpha1.SystemService_GetCurrentUser_FullMethodName:               auth.AccessRead,
		apiv1alpha1.SystemService_ListNamespaces_FullMethodName:               auth.AccessRead,
		apiv1alpha1.SystemService_GetSubstrateSummary_FullMethodName:          auth.AccessRead,
		apiv1alpha1.SystemService_ListSubstrateActors_FullMethodName:          auth.AccessRead,
		apiv1alpha1.SystemService_ListSubstrateWorkers_FullMethodName:         auth.AccessRead,
		apiv1alpha1.MemoryService_AddSession_FullMethodName:                   auth.AccessCreate,
		apiv1alpha1.MemoryService_AddSessionBatch_FullMethodName:              auth.AccessCreate,
		apiv1alpha1.MemoryService_Search_FullMethodName:                       auth.AccessRead,
		apiv1alpha1.MemoryService_List_FullMethodName:                         auth.AccessRead,
		apiv1alpha1.MemoryService_Delete_FullMethodName:                       auth.AccessDelete,
		apiv1alpha1.ModelService_ListModelConfigs_FullMethodName:              auth.AccessRead,
		apiv1alpha1.ModelService_GetModelConfig_FullMethodName:                auth.AccessRead,
		apiv1alpha1.ModelService_CreateModelConfig_FullMethodName:             auth.AccessCreate,
		apiv1alpha1.ModelService_UpdateModelConfig_FullMethodName:             auth.AccessUpdate,
		apiv1alpha1.ModelService_DeleteModelConfig_FullMethodName:             auth.AccessDelete,
		apiv1alpha1.ModelService_ListSupportedModelProviders_FullMethodName:   auth.AccessRead,
		apiv1alpha1.ModelService_ListConfiguredProviders_FullMethodName:       auth.AccessRead,
		apiv1alpha1.ModelService_ListProviderModels_FullMethodName:            auth.AccessRead,
		apiv1alpha1.ModelService_ListSupportedModels_FullMethodName:           auth.AccessRead,
		apiv1alpha1.ToolService_ListTools_FullMethodName:                      auth.AccessRead,
		apiv1alpha1.ToolService_ListToolServers_FullMethodName:                auth.AccessRead,
		apiv1alpha1.ToolService_CreateToolServer_FullMethodName:               auth.AccessCreate,
		apiv1alpha1.ToolService_DeleteToolServer_FullMethodName:               auth.AccessDelete,
		apiv1alpha1.ToolService_ListToolServerTypes_FullMethodName:            auth.AccessRead,
		apiv1alpha1.ToolService_ListMCPAppTools_FullMethodName:                auth.AccessRead,
		apiv1alpha1.ToolService_CallMCPAppTool_FullMethodName:                 auth.AccessCreate,
		apiv1alpha1.ToolService_ReadMCPAppResource_FullMethodName:             auth.AccessRead,
		apiv1alpha1.PromptTemplateService_ListPromptTemplates_FullMethodName:  auth.AccessRead,
		apiv1alpha1.PromptTemplateService_GetPromptTemplate_FullMethodName:    auth.AccessRead,
		apiv1alpha1.PromptTemplateService_CreatePromptTemplate_FullMethodName: auth.AccessCreate,
		apiv1alpha1.PromptTemplateService_UpdatePromptTemplate_FullMethodName: auth.AccessUpdate,
		apiv1alpha1.PromptTemplateService_DeletePromptTemplate_FullMethodName: auth.AccessDelete,
		grpc_health_v1.Health_Check_FullMethodName:                            auth.AccessPublic,
		grpc_health_v1.Health_List_FullMethodName:                             auth.AccessPublic,
		grpc_health_v1.Health_Watch_FullMethodName:                            auth.AccessPublic,
		"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo":           auth.AccessPublic,
		"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo":      auth.AccessPublic,
		apiv1alpha1.AgentService_ListAgents_FullMethodName:                    auth.AccessRead,
		apiv1alpha1.AgentService_GetAgent_FullMethodName:                      auth.AccessRead,
		apiv1alpha1.AgentService_CreateAgent_FullMethodName:                   auth.AccessCreate,
		apiv1alpha1.AgentService_UpdateAgent_FullMethodName:                   auth.AccessUpdate,
		apiv1alpha1.AgentService_DeleteAgent_FullMethodName:                   auth.AccessDelete,
		apiv1alpha1.AgentTemplateService_ListAgentTemplates_FullMethodName:    auth.AccessRead,
		apiv1alpha1.AgentTemplateService_GetAgentTemplate_FullMethodName:      auth.AccessRead,
		apiv1alpha1.AgentTemplateService_CreateAgentTemplate_FullMethodName:   auth.AccessCreate,
		apiv1alpha1.AgentTemplateService_UpdateAgentTemplate_FullMethodName:   auth.AccessUpdate,
		apiv1alpha1.AgentTemplateService_DeleteAgentTemplate_FullMethodName:   auth.AccessDelete,
		apiv1alpha1.HarnessService_ListHarnesses_FullMethodName:               auth.AccessRead,
		apiv1alpha1.HarnessService_CreateHarness_FullMethodName:               auth.AccessCreate,
		apiv1alpha1.HarnessService_DeleteHarness_FullMethodName:               auth.AccessDelete,

		apiv1alpha1.SandboxTemplateService_ListSandboxTemplates_FullMethodName:  auth.AccessRead,
		apiv1alpha1.SandboxTemplateService_CreateSandboxTemplate_FullMethodName: auth.AccessCreate,
		apiv1alpha1.SandboxTemplateService_DeleteSandboxTemplate_FullMethodName: auth.AccessDelete,
	}
	policies[apiv1alpha1.SessionService_CreateSession_FullMethodName] = auth.AccessCreate
	policies[apiv1alpha1.SessionService_GetSession_FullMethodName] = auth.AccessRead
	policies[apiv1alpha1.SessionService_ListSessions_FullMethodName] = auth.AccessRead
	// A rename is the only write on this service that is not a lifecycle
	// operation, and it must not inherit the read mode its neighbours carry.
	policies[apiv1alpha1.SessionService_UpdateSessionName_FullMethodName] = auth.AccessUpdate
	policies[apiv1alpha1.SessionService_SuspendSession_FullMethodName] = auth.AccessUpdate
	policies[apiv1alpha1.SessionService_ResumeSession_FullMethodName] = auth.AccessUpdate
	policies[apiv1alpha1.SessionService_DeleteSession_FullMethodName] = auth.AccessDelete
	policies[apiv1alpha1.SessionService_CreateSessionShare_FullMethodName] = auth.AccessCreate
	policies[apiv1alpha1.SessionService_ListSessionShares_FullMethodName] = auth.AccessRead
	policies[apiv1alpha1.SessionService_RevokeSessionShare_FullMethodName] = auth.AccessDelete
	policies[apiv1alpha1.CheckpointService_CreateCheckpoint_FullMethodName] = auth.AccessCreate
	policies[apiv1alpha1.CheckpointService_GetCheckpoint_FullMethodName] = auth.AccessRead
	policies[apiv1alpha1.CheckpointService_ListCheckpoints_FullMethodName] = auth.AccessRead
	policies[apiv1alpha1.CheckpointService_DeleteCheckpoint_FullMethodName] = auth.AccessDelete
	policies[apiv1alpha1.CheckpointService_ForkSession_FullMethodName] = auth.AccessCreate
	policies[apiv1alpha1.CheckpointService_UpdateCheckpointName_FullMethodName] = auth.AccessUpdate
	policies[a2apb.A2AService_SendMessage_FullMethodName] = auth.AccessCreate
	policies[a2apb.A2AService_SendStreamingMessage_FullMethodName] = auth.AccessCreate
	policies[a2apb.A2AService_GetTask_FullMethodName] = auth.AccessRead
	policies[a2apb.A2AService_ListTasks_FullMethodName] = auth.AccessRead
	policies[a2apb.A2AService_CancelTask_FullMethodName] = auth.AccessUpdate
	policies[a2apb.A2AService_SubscribeToTask_FullMethodName] = auth.AccessRead
	policies[a2apb.A2AService_CreateTaskPushNotificationConfig_FullMethodName] = auth.AccessCreate
	policies[a2apb.A2AService_GetTaskPushNotificationConfig_FullMethodName] = auth.AccessRead
	policies[a2apb.A2AService_ListTaskPushNotificationConfigs_FullMethodName] = auth.AccessRead
	policies[a2apb.A2AService_DeleteTaskPushNotificationConfig_FullMethodName] = auth.AccessDelete
	policies[a2apb.A2AService_GetExtendedAgentCard_FullMethodName] = auth.AccessRead
	policies[apiv1alpha1.ScheduledRunService_CreateScheduledRun_FullMethodName] = auth.AccessCreate
	policies[apiv1alpha1.ScheduledRunService_GetScheduledRun_FullMethodName] = auth.AccessRead
	policies[apiv1alpha1.ScheduledRunService_UpdateScheduledRun_FullMethodName] = auth.AccessUpdate
	policies[apiv1alpha1.ScheduledRunService_DeleteScheduledRun_FullMethodName] = auth.AccessDelete
	policies[apiv1alpha1.ScheduledRunService_TriggerScheduledRun_FullMethodName] = auth.AccessCreate
	policies[apiv1alpha1.ScheduledRunService_ListScheduledRuns_FullMethodName] = auth.AccessRead
	policies[apiv1alpha1.ScheduledRunService_GetScheduledRunExecution_FullMethodName] = auth.AccessRead
	policies[apiv1alpha1.ScheduledRunService_ListScheduledRunExecutions_FullMethodName] = auth.AccessRead
	policies[apiv1alpha1.SandboxService_CreateSandbox_FullMethodName] = auth.AccessCreate
	policies[apiv1alpha1.SandboxService_GetSandbox_FullMethodName] = auth.AccessRead
	policies[apiv1alpha1.SandboxService_ListSandboxes_FullMethodName] = auth.AccessRead
	policies[apiv1alpha1.SandboxService_SuspendSandbox_FullMethodName] = auth.AccessUpdate
	policies[apiv1alpha1.SandboxService_ResumeSandbox_FullMethodName] = auth.AccessUpdate
	policies[apiv1alpha1.SandboxService_DeleteSandbox_FullMethodName] = auth.AccessDelete
	policies[guestpb.ProcessService_StartProcess_FullMethodName] = auth.AccessCreate
	policies[guestpb.ProcessService_GetProcess_FullMethodName] = auth.AccessRead
	policies[guestpb.ProcessService_KillProcess_FullMethodName] = auth.AccessUpdate
	policies[guestpb.ProcessService_StreamProcessOutputs_FullMethodName] = auth.AccessRead
	policies[guestpb.FileSystemService_ReadFile_FullMethodName] = auth.AccessRead
	policies[guestpb.FileSystemService_WriteFile_FullMethodName] = auth.AccessUpdate
	return policies
}
