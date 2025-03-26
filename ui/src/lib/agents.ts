/* eslint-disable @typescript-eslint/no-explicit-any */
import {
  AgentConfig,
  AssistantAgentConfig,
  Component,
  ComponentConfig,
  Team,
  TeamConfig,
} from "@/types/datamodel";

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
