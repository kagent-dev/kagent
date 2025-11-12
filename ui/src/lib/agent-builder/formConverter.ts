import type { Tool, ToolNodeData, VisualNode, AgentFormData } from "@/types";

/**
 * Convert Tool array to ToolNodeData array
 * @param tools Array of tools
 * @returns Array of ToolNodeData
 */
export function convertToolsToNodes(tools: Tool[]): ToolNodeData[] {
  if (!tools || tools.length === 0) {
    return [];
  }

  // Group tools by server/agent reference
  const groupedTools: Record<string, Tool[]> = {};

  tools.forEach(tool => {
    let key = 'default';
    
    if (tool.type === 'McpServer' && tool.mcpServer) {
      key = tool.mcpServer.name || 'default';
    } else if (tool.type === 'Agent' && tool.agent) {
      key = tool.agent.name || 'default';
    }

    if (!groupedTools[key]) {
      groupedTools[key] = [];
    }
    groupedTools[key].push(tool);
  });

  // Convert grouped tools to ToolNodeData
  return Object.entries(groupedTools).map(([serverRef, toolList]) => ({
    serverRef,
    tools: toolList,
  }));
}

/**
 * Convert ToolNodeData array back to Tool array
 * @param nodeData Array of ToolNodeData
 * @returns Array of tools
 */
export function convertNodesToTools(nodeData: ToolNodeData[]): Tool[] {
  const allTools: Tool[] = [];

  nodeData.forEach(node => {
    if (node.tools && Array.isArray(node.tools)) {
      allTools.push(...node.tools);
    }
  });

  return allTools;
}

/**
 * Merge multiple node data into single AgentFormData
 * @param nodes All visual nodes
 * @returns Partial AgentFormData with merged data
 */
export function mergeNodeData(nodes: VisualNode[]): Partial<AgentFormData> {
  const result: Partial<AgentFormData> = {
    tools: [],
  };

  nodes.forEach(node => {
    switch (node.type) {
      case 'basic-info':
        result.name = (node.data.name as string) || result.name;
        result.namespace = (node.data.namespace as string) || result.namespace;
        result.description = (node.data.description as string) || result.description;
        break;

      case 'system-prompt':
        result.systemPrompt = (node.data.systemPrompt as string) || result.systemPrompt;
        break;

      case 'llm':
        result.modelName = (node.data.modelName as string) || result.modelName;
        result.stream = (node.data.stream as boolean) !== false;
        break;

      case 'tool':
        const tools = node.data.tools as Tool[];
        if (tools && Array.isArray(tools)) {
          result.tools = [...(result.tools || []), ...tools];
        }
        break;

      case 'output':
        if (node.data.streaming !== undefined) {
          result.stream = node.data.streaming as boolean;
        }
        break;
    }
  });

  return result;
}

/**
 * Extract basic info from nodes
 * @param nodes All visual nodes
 * @returns Basic info object
 */
export function extractBasicInfo(nodes: VisualNode[]): {
  name: string;
  namespace: string;
  description: string;
} {
  const basicInfoNode = nodes.find(node => node.type === 'basic-info');

  if (basicInfoNode) {
    return {
      name: (basicInfoNode.data.name as string) || '',
      namespace: (basicInfoNode.data.namespace as string) || 'default',
      description: (basicInfoNode.data.description as string) || '',
    };
  }

  return {
    name: '',
    namespace: 'default',
    description: '',
  };
}

/**
 * Check if form data is complete enough to create an agent
 * @param formData Agent form data
 * @returns True if complete, false otherwise
 */
export function isFormDataComplete(formData: Partial<AgentFormData>): boolean {
  return !!(
    formData.name &&
    formData.namespace &&
    formData.modelName &&
    formData.systemPrompt
  );
}

/**
 * Sanitize tool data to ensure proper structure
 * @param tools Array of tools
 * @returns Sanitized array of tools
 */
export function sanitizeTools(tools: Tool[]): Tool[] {
  if (!Array.isArray(tools)) {
    return [];
  }

  return tools.filter(tool => {
    // Ensure tool has a valid type
    if (!tool.type || (tool.type !== 'McpServer' && tool.type !== 'Agent')) {
      return false;
    }

    // For McpServer type, ensure mcpServer data exists
    if (tool.type === 'McpServer') {
      return !!(tool.mcpServer && tool.mcpServer.name);
    }

    // For Agent type, ensure agent data exists
    if (tool.type === 'Agent') {
      return !!(tool.agent && tool.agent.name);
    }

    return false;
  });
}

