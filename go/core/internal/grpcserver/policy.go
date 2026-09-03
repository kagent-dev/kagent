package grpcserver

import (
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

type MethodPolicies map[string]auth.AccessMode

func DefaultMethodPolicies() MethodPolicies {
	policies := MethodPolicies{
		apiv1alpha1.SystemService_GetVersion_FullMethodName:                   auth.AccessPublic,
		apiv1alpha1.SystemService_GetCurrentUser_FullMethodName:               auth.AccessRead,
		apiv1alpha1.SystemService_ListNamespaces_FullMethodName:               auth.AccessRead,
		apiv1alpha1.SystemService_GetSubstrateStatus_FullMethodName:           auth.AccessRead,
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
		apiv1alpha1.ModelService_ListSupportedMemoryProviders_FullMethodName:  auth.AccessRead,
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
		apiv1alpha1.AgentTemplateService_ListAgentTemplates_FullMethodName:    auth.AccessRead,
		apiv1alpha1.AgentTemplateService_GetAgentTemplate_FullMethodName:      auth.AccessRead,
		apiv1alpha1.AgentTemplateService_CreateAgentTemplate_FullMethodName:   auth.AccessCreate,
		apiv1alpha1.AgentTemplateService_UpdateAgentTemplate_FullMethodName:   auth.AccessUpdate,
		apiv1alpha1.AgentTemplateService_DeleteAgentTemplate_FullMethodName:   auth.AccessDelete,
		apiv1alpha1.HarnessService_ListHarnesses_FullMethodName:               auth.AccessRead,
		apiv1alpha1.HarnessService_CreateHarness_FullMethodName:               auth.AccessCreate,
		apiv1alpha1.HarnessService_DeleteHarness_FullMethodName:               auth.AccessDelete,
	}
	policies[apiv1alpha1.AgentInstanceService_CreateAgentInstance_FullMethodName] = auth.AccessCreate
	policies[apiv1alpha1.AgentInstanceService_GetAgentInstance_FullMethodName] = auth.AccessRead
	policies[apiv1alpha1.AgentInstanceService_ListAgentInstances_FullMethodName] = auth.AccessRead
	// A rename is the only write on this service that is not a lifecycle
	// operation, and it must not inherit the read mode its neighbours carry.
	policies[apiv1alpha1.AgentInstanceService_UpdateAgentInstanceName_FullMethodName] = auth.AccessUpdate
	policies[apiv1alpha1.AgentInstanceService_SuspendAgentInstance_FullMethodName] = auth.AccessUpdate
	policies[apiv1alpha1.AgentInstanceService_ResumeAgentInstance_FullMethodName] = auth.AccessUpdate
	policies[apiv1alpha1.AgentInstanceService_DeleteAgentInstance_FullMethodName] = auth.AccessDelete
	policies[apiv1alpha1.AgentInstanceService_CreateAgentInstanceShare_FullMethodName] = auth.AccessCreate
	policies[apiv1alpha1.AgentInstanceService_ListAgentInstanceShares_FullMethodName] = auth.AccessRead
	policies[apiv1alpha1.AgentInstanceService_RevokeAgentInstanceShare_FullMethodName] = auth.AccessDelete
	policies[apiv1alpha1.CheckpointService_CreateCheckpoint_FullMethodName] = auth.AccessCreate
	policies[apiv1alpha1.CheckpointService_GetCheckpoint_FullMethodName] = auth.AccessRead
	policies[apiv1alpha1.CheckpointService_ListCheckpoints_FullMethodName] = auth.AccessRead
	policies[apiv1alpha1.CheckpointService_DeleteCheckpoint_FullMethodName] = auth.AccessDelete
	policies[apiv1alpha1.CheckpointService_ForkAgentInstance_FullMethodName] = auth.AccessCreate
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
	return policies
}
