/**
 * The data layer's public surface.
 *
 * Pages and components import from here and nowhere deeper. They never learn
 * whether the mock backend or a real cluster is answering — that decision lives
 * in `config.ts` — nor that the application API is gRPC-Web while a conversation
 * is still held over HTTP.
 */

export { ApiError, isApiError, isNotFound } from "./ApiError";
export type { ApiErrorKind } from "./ApiError";

export { apiBaseUrl, apiMode, isMockMode, MOCK_API_BASE_URL } from "./config";
export type { ApiMode } from "./config";

export { apiClient, createApiClient } from "./client";
export type {
  KagentApiClient,
  McpServersApi,
  ModelsApi,
  AgentInstancesApi,
  NamespacesApi,
  PromptsApi,
  SubstrateApi,
  ReadOptions,
} from "./client";

export { invoke, operationIds } from "./operations";
export type {
  AgentInstanceRef,
  ApiOperation,
  ApiOperations,
  OperationCallOptions,
  OperationId,
  OperationInput,
  OperationMap,
  OperationOutput,
  SubstrateActorSortField,
  SubstratePageInput,
  SubstrateSortOrder,
  SubstrateWorkerSortField,
} from "./operations";

export {
  clearApiExtensions,
  registerApiTransform,
  registerOperationOverride,
} from "./extensionPoints";
export type {
  ApiCallId,
  ApiRequestContext,
  ApiResponseContext,
  ApiTransform,
} from "./extensionPoints";

export {
  hasLiveBackend,
  registerApiBaseUrlResolver,
  registerAuthTokenSource,
  resetApiTransport,
  setApiTransport,
} from "./transport";
export type { AuthTokenSource } from "./transport";

export * from "./domain/agentInstances";
export * from "./domain/common";
export * from "./domain/mcpServers";
export * from "./domain/models";
export * from "./domain/namespaces";
export * from "./domain/substrate";
export * from "./domain/prompts";
export * from "./domain/harnesses";
export * from "./domain/agentTemplates";
export * from "./domain/agentPairs";

export { useMcpServers, useTools } from "./hooks/useMcpServers";
export {
  useModel,
  useModels,
  useProviderModels,
  useProviders,
} from "./hooks/useModels";
export { usePrompt, usePrompts } from "./hooks/usePrompts";
export { useNamespaces } from "./hooks/useNamespaces";
export {
  useSubstrateActors,
  useSubstrateStatus,
  useSubstrateSummary,
  useSubstrateWorkers,
} from "./hooks/useSubstrate";
export {
  partitionByAdmission,
  useAgentTemplate,
  useAgentTemplates,
  useAgentTemplatesAcrossNamespaces,
  useHarnesses,
  useHarnessesAcrossNamespaces,
} from "./hooks/useAgentBuildingBlocks";
export {
  useAgentConversations,
  useAgentInstance,
  useAgentInstances,
  useAgentInstancesAcrossNamespaces,
} from "./hooks/useAgentInstances";
export { useInvalidateConversations } from "./hooks/useInvalidateConversations";
export { useInvalidatePrompts } from "./hooks/useInvalidatePrompts";
export type {
  AgentConversations,
  AgentInstancesAcrossNamespaces,
} from "./hooks/useAgentInstances";
export { useApiResource } from "./hooks/useApiResource";
export type { ApiResource } from "./hooks/useApiResource";

export { useChat } from "./hooks/useChat";
export type { ChatController, ChatPhase } from "./hooks/useChat";
export type { ChatTurnPhase } from "./chat/turnMachine";
export type { HitlQuestion, HitlTool, PendingRequest } from "./chat/hitl";
export { HITL_EXTENSION_URI } from "./chat/hitl";

export { getChatClient, resetChatClient, setChatClientFactory } from "./chat";
export type {
  ChatClient,
  ChatDataPart,
  ChatEvent,
  ChatMessage,
  ChatPart,
  ChatRole,
  ChatTextPart,
  ChatTurnState,
} from "./chat";
