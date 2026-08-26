package grpcserver

import (
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

type AccessMode string

const (
	AccessPublic AccessMode = "public"
	AccessRead   AccessMode = "read"
	AccessCreate AccessMode = "create"
	AccessUpdate AccessMode = "update"
	AccessDelete AccessMode = "delete"
)

type MethodPolicies map[string]AccessMode

func DefaultMethodPolicies() MethodPolicies {
	policies := MethodPolicies{
		apiv1alpha1.SystemService_GetVersion_FullMethodName:                   AccessPublic,
		apiv1alpha1.SystemService_GetCurrentUser_FullMethodName:               AccessRead,
		apiv1alpha1.SystemService_ListNamespaces_FullMethodName:               AccessRead,
		apiv1alpha1.SystemService_GetSubstrateStatus_FullMethodName:           AccessRead,
		apiv1alpha1.FeedbackService_CreateFeedback_FullMethodName:             AccessCreate,
		apiv1alpha1.FeedbackService_ListFeedback_FullMethodName:               AccessRead,
		apiv1alpha1.MemoryService_AddSession_FullMethodName:                   AccessCreate,
		apiv1alpha1.MemoryService_AddSessionBatch_FullMethodName:              AccessCreate,
		apiv1alpha1.MemoryService_Search_FullMethodName:                       AccessRead,
		apiv1alpha1.MemoryService_List_FullMethodName:                         AccessRead,
		apiv1alpha1.MemoryService_Delete_FullMethodName:                       AccessDelete,
		apiv1alpha1.SessionService_ListSessions_FullMethodName:                AccessRead,
		apiv1alpha1.SessionService_ListSessionsByAgent_FullMethodName:         AccessRead,
		apiv1alpha1.SessionService_CreateSession_FullMethodName:               AccessCreate,
		apiv1alpha1.SessionService_GetSession_FullMethodName:                  AccessRead,
		apiv1alpha1.SessionService_UpdateSession_FullMethodName:               AccessUpdate,
		apiv1alpha1.SessionService_DeleteSession_FullMethodName:               AccessDelete,
		apiv1alpha1.SessionService_AddSessionEvent_FullMethodName:             AccessCreate,
		apiv1alpha1.SessionService_CreateSessionShare_FullMethodName:          AccessCreate,
		apiv1alpha1.SessionService_ListSessionShares_FullMethodName:           AccessRead,
		apiv1alpha1.SessionService_DeleteSessionShare_FullMethodName:          AccessDelete,
		apiv1alpha1.TaskStoreService_UpsertTask_FullMethodName:                AccessUpdate,
		apiv1alpha1.TaskStoreService_GetTask_FullMethodName:                   AccessRead,
		apiv1alpha1.TaskStoreService_DeleteTask_FullMethodName:                AccessDelete,
		apiv1alpha1.TaskStoreService_ListTasks_FullMethodName:                 AccessRead,
		apiv1alpha1.AgentService_ListAgents_FullMethodName:                    AccessRead,
		apiv1alpha1.AgentService_GetSandboxAgent_FullMethodName:               AccessRead,
		apiv1alpha1.AgentService_CreateSandboxAgent_FullMethodName:            AccessCreate,
		apiv1alpha1.AgentService_UpdateSandboxAgent_FullMethodName:            AccessUpdate,
		apiv1alpha1.AgentService_DeleteSandboxAgent_FullMethodName:            AccessDelete,
		apiv1alpha1.AgentService_GetAgentHarness_FullMethodName:               AccessRead,
		apiv1alpha1.AgentService_CreateAgentHarness_FullMethodName:            AccessCreate,
		apiv1alpha1.AgentService_DeleteAgentHarness_FullMethodName:            AccessDelete,
		apiv1alpha1.ModelService_ListModelConfigs_FullMethodName:              AccessRead,
		apiv1alpha1.ModelService_GetModelConfig_FullMethodName:                AccessRead,
		apiv1alpha1.ModelService_CreateModelConfig_FullMethodName:             AccessCreate,
		apiv1alpha1.ModelService_UpdateModelConfig_FullMethodName:             AccessUpdate,
		apiv1alpha1.ModelService_DeleteModelConfig_FullMethodName:             AccessDelete,
		apiv1alpha1.ModelService_ListSupportedModelProviders_FullMethodName:   AccessRead,
		apiv1alpha1.ModelService_ListSupportedMemoryProviders_FullMethodName:  AccessRead,
		apiv1alpha1.ModelService_ListConfiguredProviders_FullMethodName:       AccessRead,
		apiv1alpha1.ModelService_ListProviderModels_FullMethodName:            AccessRead,
		apiv1alpha1.ModelService_ListSupportedModels_FullMethodName:           AccessRead,
		apiv1alpha1.ToolService_ListTools_FullMethodName:                      AccessRead,
		apiv1alpha1.ToolService_ListToolServers_FullMethodName:                AccessRead,
		apiv1alpha1.ToolService_CreateToolServer_FullMethodName:               AccessCreate,
		apiv1alpha1.ToolService_DeleteToolServer_FullMethodName:               AccessDelete,
		apiv1alpha1.ToolService_ListToolServerTypes_FullMethodName:            AccessRead,
		apiv1alpha1.ToolService_ListMCPAppTools_FullMethodName:                AccessRead,
		apiv1alpha1.ToolService_CallMCPAppTool_FullMethodName:                 AccessCreate,
		apiv1alpha1.ToolService_ReadMCPAppResource_FullMethodName:             AccessRead,
		apiv1alpha1.PromptTemplateService_ListPromptTemplates_FullMethodName:  AccessRead,
		apiv1alpha1.PromptTemplateService_GetPromptTemplate_FullMethodName:    AccessRead,
		apiv1alpha1.PromptTemplateService_CreatePromptTemplate_FullMethodName: AccessCreate,
		apiv1alpha1.PromptTemplateService_UpdatePromptTemplate_FullMethodName: AccessUpdate,
		apiv1alpha1.PromptTemplateService_DeletePromptTemplate_FullMethodName: AccessDelete,
		grpc_health_v1.Health_Check_FullMethodName:                            AccessPublic,
		grpc_health_v1.Health_List_FullMethodName:                             AccessPublic,
		grpc_health_v1.Health_Watch_FullMethodName:                            AccessPublic,
		"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo":           AccessPublic,
		"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo":      AccessPublic,
		apiv1alpha1.SystemService_GetSubstrateSummary_FullMethodName:          AccessRead,
		apiv1alpha1.SystemService_ListSubstrateActors_FullMethodName:          AccessRead,
		apiv1alpha1.SystemService_ListSubstrateWorkers_FullMethodName:         AccessRead,
		apiv1alpha1.AgentTemplateService_ListAgentTemplates_FullMethodName:    AccessRead,
		apiv1alpha1.AgentTemplateService_GetAgentTemplate_FullMethodName:      AccessRead,
		apiv1alpha1.AgentTemplateService_CreateAgentTemplate_FullMethodName:   AccessCreate,
		apiv1alpha1.AgentTemplateService_UpdateAgentTemplate_FullMethodName:   AccessUpdate,
		apiv1alpha1.AgentTemplateService_DeleteAgentTemplate_FullMethodName:   AccessDelete,
		apiv1alpha1.HarnessService_ListHarnesses_FullMethodName:               AccessRead,
		apiv1alpha1.HarnessService_GetHarness_FullMethodName:                  AccessRead,
		apiv1alpha1.HarnessService_CreateHarness_FullMethodName:               AccessCreate,
		apiv1alpha1.HarnessService_UpdateHarness_FullMethodName:               AccessUpdate,
		apiv1alpha1.HarnessService_DeleteHarness_FullMethodName:               AccessDelete,
	}
	policies[apiv1alpha1.AgentInstanceService_CreateAgentInstance_FullMethodName] = AccessCreate
	policies[apiv1alpha1.AgentInstanceService_GetAgentInstance_FullMethodName] = AccessRead
	policies[apiv1alpha1.AgentInstanceService_ListAgentInstances_FullMethodName] = AccessRead
	// A rename is the only write on this service that is not a lifecycle
	// operation, and it must not inherit the read mode its neighbours carry.
	policies[apiv1alpha1.AgentInstanceService_RenameAgentInstance_FullMethodName] = AccessUpdate
	policies[apiv1alpha1.AgentInstanceService_SuspendAgentInstance_FullMethodName] = AccessUpdate
	policies[apiv1alpha1.AgentInstanceService_ResumeAgentInstance_FullMethodName] = AccessUpdate
	policies[apiv1alpha1.AgentInstanceService_DeleteAgentInstance_FullMethodName] = AccessDelete
	policies[apiv1alpha1.AgentInstanceService_CreateAgentInstanceShare_FullMethodName] = AccessCreate
	policies[apiv1alpha1.AgentInstanceService_ListAgentInstanceShares_FullMethodName] = AccessRead
	policies[apiv1alpha1.AgentInstanceService_RevokeAgentInstanceShare_FullMethodName] = AccessDelete
	policies[apiv1alpha1.CheckpointService_CreateCheckpoint_FullMethodName] = AccessCreate
	policies[apiv1alpha1.CheckpointService_GetCheckpoint_FullMethodName] = AccessRead
	policies[apiv1alpha1.CheckpointService_ListCheckpoints_FullMethodName] = AccessRead
	policies[apiv1alpha1.CheckpointService_DeleteCheckpoint_FullMethodName] = AccessDelete
	policies[apiv1alpha1.CheckpointService_ForkAgentInstance_FullMethodName] = AccessCreate
	policies[a2apb.A2AService_SendMessage_FullMethodName] = AccessCreate
	policies[a2apb.A2AService_SendStreamingMessage_FullMethodName] = AccessCreate
	policies[a2apb.A2AService_GetTask_FullMethodName] = AccessRead
	policies[a2apb.A2AService_ListTasks_FullMethodName] = AccessRead
	policies[a2apb.A2AService_CancelTask_FullMethodName] = AccessUpdate
	policies[a2apb.A2AService_SubscribeToTask_FullMethodName] = AccessRead
	policies[a2apb.A2AService_CreateTaskPushNotificationConfig_FullMethodName] = AccessCreate
	policies[a2apb.A2AService_GetTaskPushNotificationConfig_FullMethodName] = AccessRead
	policies[a2apb.A2AService_ListTaskPushNotificationConfigs_FullMethodName] = AccessRead
	policies[a2apb.A2AService_DeleteTaskPushNotificationConfig_FullMethodName] = AccessDelete
	policies[a2apb.A2AService_GetExtendedAgentCard_FullMethodName] = AccessRead
	return policies
}
