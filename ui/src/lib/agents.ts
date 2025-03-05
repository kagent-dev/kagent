/* eslint-disable @typescript-eslint/no-explicit-any */
import { AgentConfig, AssistantAgentConfig, ChatCompletionContextConfig, Component, ComponentConfig, ModelConfig, OpenAIClientConfig, RoundRobinGroupChatConfig, Team, TeamConfig, UserProxyAgentConfig } from "@/types/datamodel";
import { getCurrentUserId } from "@/app/actions/utils";
import { AgentFormData } from "@/components/AgentsProvider";

export const createTeamConfig = async (agentConfig: Component<AssistantAgentConfig>): Promise<Team> => {
  const userProxyConfig: Component<UserProxyAgentConfig> = {
    provider: "autogen_agentchat.agents.UserProxyAgent",
    component_type: "agent",
    version: 1,
    component_version: 1,
    description: "An agent that represents a user.",
    label: "kagent_user",
    config: {
      name: "kagent_user",
      description: "Human user",
    },
  };

  const groupChatConfig: Component<RoundRobinGroupChatConfig> = {
    provider: "autogen_agentchat.teams.RoundRobinGroupChat",
    component_type: "team",
    version: 1,
    component_version: 1,
    description: agentConfig.config.description,
    label: agentConfig.config.name,
    config: {
      participants: [agentConfig, userProxyConfig],
      model_client: agentConfig.config.model_client,
      termination_condition: {
        provider: "autogen_agentchat.conditions.TextMentionTermination",
        component_type: "termination",
        version: 1,
        component_version: 1,
        description: "Terminate the conversation if a specific text is mentioned.",
        label: "TextMentionTermination",
        config: {
          text: "TERMINATE",
        },
      },
    },
  };

  const userId = await getCurrentUserId();
  const teamConfig = {
    user_id: userId,
    version: 0,
    component: groupChatConfig,
  };
  return teamConfig;
};

export const transformToAgentConfig = (formData: AgentFormData): Component<AssistantAgentConfig> => {
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

  const modelConfig = modelClientMap[formData.model.id];
  if (!modelConfig) {
    throw new Error(`Invalid model selected: ${formData.model}`);
  }

  const modelClient: Component<ModelConfig> = {
    provider: modelConfig.provider,
    component_type: "model",
    version: 1,
    component_version: 1,
    description: "Chat completion client for model.",
    label: modelConfig.provider.split(".").pop(),
    config: {
      model: modelConfig.model,
      stream_options: {
        include_usage: true,
      },
    } as OpenAIClientConfig,
  };

  const modelContext: Component<ChatCompletionContextConfig> = {
    provider: "autogen_core.model_context.UnboundedChatCompletionContext",
    component_type: "model",
    version: 1,
    component_version: 1,
    description: "An unbounded chat completion context that keeps a view of the all the messages.",
    label: "UnboundedChatCompletionContext",
    config: {},
  };

  const agentConfig: Component<AssistantAgentConfig> = {
    provider: "autogen_agentchat.agents.AssistantAgent",
    component_type: "agent",
    version: 1,
    component_version: 1,
    description: formData.description,
    label: formData.name,
    config: {
      name: formData.name,
      description: formData.description,
      model_client: modelClient,
      tools: formData.tools,
      handoffs: [],
      model_context: modelContext,
      system_message: formData.systemPrompt,
      reflect_on_tool_use: true,
      tool_call_summary_format: "{result}",
      model_client_stream: true,
    },
  };

  return agentConfig;
};

function isAssistantAgent(component: Component<any>): boolean {
  return (
    component.provider === "autogen_agentchat.agents.AssistantAgent" &&
    !component.label?.startsWith("kagent_")
  );
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

  if ('participants' in component.config && Array.isArray(component.config.participants)) {
    return traverseComponentTree(component.config.participants, isAssistantAgent);
  } else if ('team' in component.config) {
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
function traverseComponentTree<R extends ComponentConfig>(
  components: Component<any>[],
  predicate: (component: Component<any>) => boolean
): Component<R>[] {
  if (!components || !Array.isArray(components)) {
    return [];
  }

  const results: Component<R>[] = [];

  for (const component of components) {
    // Check if current component matches predicate
    if (predicate(component)) {
      results.push(component as Component<R>);
    }
    
    // Check SocietyOfMindAgent with nested team
    if (
      component.provider === "kagent.agents.SocietyOfMindAgent" && 
      component.config?.team?.config?.participants
    ) {
      const nestedResults = traverseComponentTree<R>(
        component.config.team.config.participants,
        predicate
      );
      results.push(...nestedResults);
    }
    
    // Check any other nested participants
    if (component.config?.participants) {
      const nestedResults = traverseComponentTree<R>(
        component.config.participants,
        predicate
      );
      results.push(...nestedResults);
    }
  }

  return results;
}

export function getUsersAgentFromTeam(team: Team): Component<AssistantAgentConfig> {
  if (!team.component?.config) {
    throw new Error("Invalid team structure or missing configuration");
  }
  
  if (!('participants' in team.component.config) || !Array.isArray(team.component.config.participants)) {
    throw new Error("Team configuration does not contain participants");
  }
  
  // Use the generic traversal with a find operation instead of collecting all
  const agents = traverseComponentTree<AssistantAgentConfig>(
    team.component.config.participants,
    isAssistantAgent
  );
  
  if (agents.length === 0) {
    throw new Error("No AssistantAgent found in the team hierarchy");
  }
  
  return agents[0];
}

export function updateUsersAgent(
  team: Team,
  updateFn: (agent: Component<AssistantAgentConfig>) => void
): Team {
  const teamCopy = structuredClone(team);
  
  if (!teamCopy.component?.config) {
    throw new Error("Invalid team structure or missing configuration");
  }
  
  const usersAgent = getUsersAgentFromTeam(teamCopy);
  updateFn(usersAgent);

  return teamCopy;
}