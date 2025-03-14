/* eslint-disable @typescript-eslint/no-explicit-any */
import {
  AgentConfig,
  AssistantAgentConfig,
  ChatCompletionContextConfig,
  Component,
  ComponentConfig,
  ModelConfig,
  OpenAIClientConfig,
  RoundRobinGroupChatConfig,
  TaskAgentConfig,
  Team,
  TeamConfig,
  TextMentionTerminationConfig,
  TextMessageTerminationConfig,
  UserProxyAgentConfig,
} from "@/types/datamodel";
import { getCurrentUserId } from "@/app/actions/utils";
import { AgentFormData } from "@/components/AgentsProvider";

/**
 * Creates a model client configuration based on the selected model
 */
export const createModelClient = (modelId: string): Component<ModelConfig> => {
  const modelClientMap: Record<
    string,
    {
      provider: string;
      model: string;
      stream_options?: Record<string, unknown>;
    }
  > = {
    "gpt-4o": {
      provider: "autogen_ext.models.openai.OpenAIChatCompletionClient",
      model: "gpt-4o",
    },
    "gpt-4o-mini": {
      provider: "autogen_ext.models.openai.OpenAIChatCompletionClient",
      model: "gpt-4o-mini",
    },
  };

  const modelConfig = modelClientMap[modelId];
  if (!modelConfig) {
    throw new Error(`Invalid model selected: ${modelId}`);
  }

  return {
    provider: modelConfig.provider,
    component_type: "model",
    version: 1,
    component_version: 1,
    description: "Chat completion client for OpenAI hosted models.",
    label: "OpenAIChatCompletionClient",
    config: {
      model: modelConfig.model,
      stream_options: {
        include_usage: true,
      },
    } as OpenAIClientConfig,
  };
};

/**
 * Creates a model context configuration
 */
export const createModelContext = (): Component<ChatCompletionContextConfig> => {
  return {
    provider: "autogen_core.model_context.UnboundedChatCompletionContext",
    component_type: "chat_completion_context",
    version: 1,
    component_version: 1,
    description: "An unbounded chat completion context that keeps a view of the all the messages.",
    label: "UnboundedChatCompletionContext",
    config: {},
  };
};

/**
 * Creates a text message termination condition
 */
export const createTextTerminationCondition = (text: string): Component<TextMentionTerminationConfig> => {
  return {
    provider: "autogen_agentchat.conditions.TextMentionTermination",
    component_type: "termination",
    version: 1,
    component_version: 1,
    description: "Terminate the conversation if a specific text is mentioned.",
    label: "TextMentionTermination",
    config: {
      text: text,
    },
  };
};

/**
 * Creates a user proxy agent configuration
 */
export const createUserProxyAgent = (): Component<UserProxyAgentConfig> => {
  return {
    provider: "autogen_agentchat.agents.UserProxyAgent",
    component_type: "agent",
    version: 1,
    component_version: 1,
    description: "An agent that represents a user.",
    label: "user_proxy",
    config: {
      name: "user_proxy",
      description: "Human user",
    },
  };
};

/**
 * Creates an inner assistant agent from the form data
 */
export const createInnerAssistantAgent = (formData: AgentFormData, modelClient: Component<ModelConfig>): Component<AssistantAgentConfig> => {
  return {
    provider: "autogen_agentchat.agents.AssistantAgent",
    component_type: "agent",
    version: 1,
    component_version: 1,
    description: formData.description,
    label: formData.name,
    config: {
      name: formData.name.toLowerCase().replace(/\s+/g, "_"),
      model_client: modelClient,
      tools: formData.tools,
      handoffs: [],
      model_context: createModelContext(),
      description: formData.description,
      system_message: formData.systemPrompt,
      reflect_on_tool_use: false,
      tool_call_summary_format: "{result}",
      model_client_stream: true,
    },
  };
};

/**
 * Creates an inner team (RoundRobinGroupChat) with the assistant agent
 */
export const createInnerTeam = (assistantAgent: Component<AssistantAgentConfig>, modelClient: Component<ModelConfig>): Component<RoundRobinGroupChatConfig> => {
  const textTermination: Component<TextMessageTerminationConfig> = {
    provider: "kagent.terminations.TextMessageTermination",
    component_type: "termination",
    version: 1,
    component_version: 1,
    description: "Terminate the conversation if a specific text is mentioned.",
    label: "TextMessageTermination",
    config: {
      source: assistantAgent.config.name,
    },
  };

  return {
    provider: "autogen_agentchat.teams.RoundRobinGroupChat",
    component_type: "team",
    version: 1,
    component_version: 1,
    description: `A team containing ${assistantAgent.label}`,
    label: "RoundRobinGroupChat",
    config: {
      participants: [assistantAgent],
      termination_condition: textTermination,
      model_client: modelClient,
    },
  };
};

/**
 * Creates a TaskAgent that wraps the inner team
 */
export const createTaskAgent = (innerTeam: Component<TeamConfig>): Component<TaskAgentConfig> => {
  return {
    provider: "kagent.agents.TaskAgent",
    component_type: "agent",
    version: 1,
    component_version: 1,
    description: "An agent which runs a team of agents",
    label: "society_of_mind_agent",
    config: {
      name: "society_of_mind_agent",
      team: innerTeam,
      model_context: createModelContext(),
    },
  };
};

/**
 * Creates the outer team with the TaskAgent and the user proxy
 */
export const createOuterTeam = (label: string, taskAgent: Component<TaskAgentConfig>, modelClient: Component<ModelConfig>): Component<RoundRobinGroupChatConfig> => {
  const userProxyAgent = createUserProxyAgent();

  return {
    provider: "autogen_agentchat.teams.RoundRobinGroupChat",
    component_type: "team",
    version: 1,
    component_version: 1,
    description: taskAgent.config.team.description,
    label: label,
    config: {
      participants: [taskAgent, userProxyAgent],
      termination_condition: createTextTerminationCondition("TERMINATE"),
      model_client: modelClient,
    },
  };
};

/**
 * Main function to create a complete agent structure from form data
 */
export const createAgentStructure = async (formData: AgentFormData): Promise<Component<TeamConfig>> => {
  // Create the model client based on the selected model
  const modelClient = createModelClient(formData.model.id);

  // Create the inner assistant agent with the form data
  const innerAgent = createInnerAssistantAgent(formData, modelClient);

  // Create the inner team that contains the assistant agent
  const innerTeam = createInnerTeam(innerAgent, modelClient);

  // Create the TaskAgent that wraps the inner team
  const societyOfMindAgent = createTaskAgent(innerTeam);

  // Create the outer team with the TaskAgent and user proxy
  const outerTeam = createOuterTeam(formData.name, societyOfMindAgent, modelClient);

  return outerTeam;
};

/**
 * Creates a complete Team object ready to be saved to the database
 */
export const createTeamConfig = async (formData: AgentFormData): Promise<{ id?: number, user_id: string; version: number; component: Component<TeamConfig> }> => {
  const teamComponent = await createAgentStructure(formData);

  const userId = await getCurrentUserId();
  return {
    user_id: userId,
    version: 0,
    component: teamComponent,
  };
};

function isAssistantAgent(component: Component<any>): boolean {
  return component.provider === "autogen_agentchat.agents.AssistantAgent" && !component.label?.startsWith("kagent_");
}

/**
 * Searches for all AssistantAgents in a component hierarchy
 * @param component - The component to search within
 * @returns Array of AssistantAgent components
 */
export function findAllAssistantAgents(component?: Component<TeamConfig>): Component<AgentConfig>[] {
  if (!component?.config) {
    return [];
  }

  if ("participants" in component.config && Array.isArray(component.config.participants)) {
    return traverseComponentTree(component.config.participants, isAssistantAgent);
  } else if ("team" in component.config) {
    return findAllAssistantAgents(component.config.team);
  }

  return [];
}

/**
 * Generic function to traverse a component tree and collect components matching a predicate
 * @param components - Array of components to traverse
 * @param predicate - Function to test if a component should be included
 * @returns Array of components matching the predicate
 */
function traverseComponentTree<R extends ComponentConfig>(components: Component<any>[], predicate: (component: Component<any>) => boolean): Component<R>[] {
  if (!components || !Array.isArray(components)) {
    return [];
  }

  const results: Component<R>[] = [];

  for (const component of components) {
    // Check if current component matches predicate
    if (predicate(component)) {
      results.push(component as Component<R>);
    }

    // Check TaskAgent with nested team
    if (component.provider === "kagent.agents.TaskAgent" && component.config?.team?.config?.participants) {
      const nestedResults = traverseComponentTree<R>(component.config.team.config.participants, predicate);
      results.push(...nestedResults);
    }

    // Check any other nested participants
    if (component.config?.participants) {
      const nestedResults = traverseComponentTree<R>(component.config.participants, predicate);
      results.push(...nestedResults);
    }
  }

  return results;
}

export function getUsersAgentFromTeam(team: Team): Component<AssistantAgentConfig> {
  if (!team.component?.config) {
    throw new Error("Invalid team structure or missing configuration");
  }

  if (!("participants" in team.component.config) || !Array.isArray(team.component.config.participants)) {
    throw new Error("Team configuration does not contain participants");
  }

  // Use the generic traversal with a find operation instead of collecting all
  const agents = traverseComponentTree<AssistantAgentConfig>(team.component.config.participants, isAssistantAgent);

  if (agents.length === 0) {
    throw new Error("No AssistantAgent found in the team hierarchy");
  }

  return agents[0];
}

export function updateUsersAgent(team: Team, updateFn: (agent: Component<AssistantAgentConfig>) => void): Team {
  const teamCopy = structuredClone(team);

  if (!teamCopy.component?.config) {
    throw new Error("Invalid team structure or missing configuration");
  }

  const usersAgent = getUsersAgentFromTeam(teamCopy);
  updateFn(usersAgent);

  return teamCopy;
}
