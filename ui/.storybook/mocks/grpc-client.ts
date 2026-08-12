export type AgentKubernetesKind = "Agent" | "SandboxAgent" | "AgentHarness";

function unavailableGateway(name: string): Promise<never> {
  return Promise.reject(
    new Error(
      `${name} is unavailable in Storybook. Mock the server action used by this story.`,
    ),
  );
}

export const getSystemGrpcGateway = () => unavailableGateway("System gRPC gateway");
export const getFeedbackGrpcGateway = () => unavailableGateway("Feedback gRPC gateway");
export const getModelGrpcGateway = () => unavailableGateway("Model gRPC gateway");
export const getAgentGrpcGateway = () => unavailableGateway("Agent gRPC gateway");
export const getToolGrpcGateway = () => unavailableGateway("Tool gRPC gateway");
export const getPromptTemplateGrpcGateway = () => unavailableGateway("Prompt template gRPC gateway");
export const getSessionGrpcGateway = () => unavailableGateway("Session gRPC gateway");
export const getMemoryGrpcGateway = () => unavailableGateway("Memory gRPC gateway");
