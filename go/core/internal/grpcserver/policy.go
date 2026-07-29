package grpcserver

import (
	"github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
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
	return MethodPolicies{
		apiv1alpha1.CrewAIService_StoreMemory_FullMethodName:                    AccessCreate,
		apiv1alpha1.CrewAIService_GetMemory_FullMethodName:                      AccessRead,
		apiv1alpha1.CrewAIService_ResetMemory_FullMethodName:                    AccessDelete,
		apiv1alpha1.CrewAIService_StoreFlowState_FullMethodName:                 AccessCreate,
		apiv1alpha1.CrewAIService_GetFlowState_FullMethodName:                   AccessRead,
		apiv1alpha1.SystemService_GetVersion_FullMethodName:                     AccessPublic,
		apiv1alpha1.SystemService_GetCurrentUser_FullMethodName:                 AccessRead,
		apiv1alpha1.SystemService_ListNamespaces_FullMethodName:                 AccessRead,
		apiv1alpha1.SystemService_GetSubstrateStatus_FullMethodName:             AccessRead,
		apiv1alpha1.FeedbackService_CreateFeedback_FullMethodName:               AccessCreate,
		apiv1alpha1.FeedbackService_ListFeedback_FullMethodName:                 AccessRead,
		apiv1alpha1.LangGraphService_PutCheckpoint_FullMethodName:               AccessCreate,
		apiv1alpha1.LangGraphService_ListCheckpoints_FullMethodName:             AccessRead,
		apiv1alpha1.LangGraphService_PutWrites_FullMethodName:                   AccessCreate,
		apiv1alpha1.LangGraphService_DeleteThread_FullMethodName:                AccessDelete,
		apiv1alpha1.MemoryService_AddSession_FullMethodName:                     AccessCreate,
		apiv1alpha1.MemoryService_AddSessionBatch_FullMethodName:                AccessCreate,
		apiv1alpha1.MemoryService_Search_FullMethodName:                         AccessRead,
		apiv1alpha1.MemoryService_List_FullMethodName:                           AccessRead,
		apiv1alpha1.MemoryService_Delete_FullMethodName:                         AccessDelete,
		apiv1alpha1.SessionService_ListSessions_FullMethodName:                  AccessRead,
		apiv1alpha1.SessionService_ListSessionsByAgent_FullMethodName:           AccessRead,
		apiv1alpha1.SessionService_CreateSession_FullMethodName:                 AccessCreate,
		apiv1alpha1.SessionService_GetSession_FullMethodName:                    AccessRead,
		apiv1alpha1.SessionService_UpdateSession_FullMethodName:                 AccessUpdate,
		apiv1alpha1.SessionService_DeleteSession_FullMethodName:                 AccessDelete,
		apiv1alpha1.SessionService_AddSessionEvent_FullMethodName:               AccessCreate,
		apiv1alpha1.SessionService_CreateSessionShare_FullMethodName:            AccessCreate,
		apiv1alpha1.SessionService_ListSessionShares_FullMethodName:             AccessRead,
		apiv1alpha1.SessionService_DeleteSessionShare_FullMethodName:            AccessDelete,
		apiv1alpha1.TaskService_CreateTask_FullMethodName:                       AccessCreate,
		apiv1alpha1.TaskService_GetTask_FullMethodName:                          AccessRead,
		apiv1alpha1.TaskService_DeleteTask_FullMethodName:                       AccessDelete,
		apiv1alpha1.TaskService_ListTasks_FullMethodName:                        AccessRead,
		apiv1alpha1.AgentService_ListAgents_FullMethodName:                      AccessRead,
		apiv1alpha1.AgentService_GetAgent_FullMethodName:                        AccessRead,
		apiv1alpha1.AgentService_CreateAgent_FullMethodName:                     AccessCreate,
		apiv1alpha1.AgentService_UpdateAgent_FullMethodName:                     AccessUpdate,
		apiv1alpha1.AgentService_DeleteAgent_FullMethodName:                     AccessDelete,
		apiv1alpha1.AgentService_GetSandboxAgent_FullMethodName:                 AccessRead,
		apiv1alpha1.AgentService_CreateSandboxAgent_FullMethodName:              AccessCreate,
		apiv1alpha1.AgentService_UpdateSandboxAgent_FullMethodName:              AccessUpdate,
		apiv1alpha1.AgentService_DeleteSandboxAgent_FullMethodName:              AccessDelete,
		apiv1alpha1.AgentService_GetAgentHarness_FullMethodName:                 AccessRead,
		apiv1alpha1.AgentService_CreateAgentHarness_FullMethodName:              AccessCreate,
		apiv1alpha1.AgentService_DeleteAgentHarness_FullMethodName:              AccessDelete,
		apiv1alpha1.AgentService_EnsureAgentHarnessSessionActor_FullMethodName:  AccessCreate,
		apiv1alpha1.AgentService_SuspendAgentHarnessSessionActor_FullMethodName: AccessUpdate,
		apiv1alpha1.AgentService_GetAgentHarnessSessionActor_FullMethodName:     AccessRead,
		apiv1alpha1.ModelService_ListModelConfigs_FullMethodName:                AccessRead,
		apiv1alpha1.ModelService_GetModelConfig_FullMethodName:                  AccessRead,
		apiv1alpha1.ModelService_CreateModelConfig_FullMethodName:               AccessCreate,
		apiv1alpha1.ModelService_UpdateModelConfig_FullMethodName:               AccessUpdate,
		apiv1alpha1.ModelService_DeleteModelConfig_FullMethodName:               AccessDelete,
		apiv1alpha1.ModelService_ListSupportedModelProviders_FullMethodName:     AccessRead,
		apiv1alpha1.ModelService_ListSupportedMemoryProviders_FullMethodName:    AccessRead,
		apiv1alpha1.ModelService_ListConfiguredProviders_FullMethodName:         AccessRead,
		apiv1alpha1.ModelService_ListProviderModels_FullMethodName:              AccessRead,
		apiv1alpha1.ModelService_ListSupportedModels_FullMethodName:             AccessRead,
		apiv1alpha1.ToolService_ListTools_FullMethodName:                        AccessRead,
		apiv1alpha1.ToolService_ListToolServers_FullMethodName:                  AccessRead,
		apiv1alpha1.ToolService_CreateToolServer_FullMethodName:                 AccessCreate,
		apiv1alpha1.ToolService_DeleteToolServer_FullMethodName:                 AccessDelete,
		apiv1alpha1.ToolService_ListToolServerTypes_FullMethodName:              AccessRead,
		apiv1alpha1.ToolService_ListMCPAppTools_FullMethodName:                  AccessRead,
		apiv1alpha1.ToolService_CallMCPAppTool_FullMethodName:                   AccessCreate,
		apiv1alpha1.ToolService_ReadMCPAppResource_FullMethodName:               AccessRead,
		apiv1alpha1.PromptTemplateService_ListPromptTemplates_FullMethodName:    AccessRead,
		apiv1alpha1.PromptTemplateService_GetPromptTemplate_FullMethodName:      AccessRead,
		apiv1alpha1.PromptTemplateService_CreatePromptTemplate_FullMethodName:   AccessCreate,
		apiv1alpha1.PromptTemplateService_UpdatePromptTemplate_FullMethodName:   AccessUpdate,
		apiv1alpha1.PromptTemplateService_DeletePromptTemplate_FullMethodName:   AccessDelete,
		grpc_health_v1.Health_Check_FullMethodName:                              AccessPublic,
		grpc_health_v1.Health_List_FullMethodName:                               AccessPublic,
		grpc_health_v1.Health_Watch_FullMethodName:                              AccessPublic,
		"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo":             AccessPublic,
		"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo":        AccessPublic,
	}
}
