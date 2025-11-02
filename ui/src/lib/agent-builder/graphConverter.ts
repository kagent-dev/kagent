import type { 
  AgentFormData, 
  VisualNode, 
  VisualEdge,
  LLMNodeData,
  SystemPromptNodeData,
  ToolNodeData,
  OutputNodeData,
  BasicInfoNodeData
} from "@/types";
import { generateNodeId } from "./utils";

/**
 * Converts AgentFormData to Visual Graph (nodes + edges)
 * @param formData Agent form data
 * @returns Object containing nodes and edges arrays
 */
export function convertFormDataToGraph(formData: AgentFormData): {
  nodes: VisualNode[];
  edges: VisualEdge[];
} {
  const nodes: VisualNode[] = [];
  const edges: VisualEdge[] = [];
  let yPosition = 100;
  const ySpacing = 200;

  // 1. Create Basic Info Node
  const basicInfoId = generateNodeId('basic-info');
  nodes.push({
    id: basicInfoId,
    type: 'basic-info',
    position: { x: 100, y: yPosition },
    data: {
      name: formData.name || '',
      namespace: formData.namespace || 'default',
      description: formData.description || '',
      type: formData.type || 'Declarative',
    } as BasicInfoNodeData,
  });

  yPosition += ySpacing;

  // 2. Create System Prompt Node (if exists)
  let systemPromptId: string | null = null;
  if (formData.systemPrompt) {
    systemPromptId = generateNodeId('system-prompt');
    nodes.push({
      id: systemPromptId,
      type: 'system-prompt',
      position: { x: 100, y: yPosition },
      data: {
        systemPrompt: formData.systemPrompt,
      } as SystemPromptNodeData,
    });

    // Connect basic-info to system-prompt
    edges.push({
      id: `edge-${basicInfoId}-${systemPromptId}`,
      source: basicInfoId,
      target: systemPromptId,
      animated: false,
    });

    yPosition += ySpacing;
  }

  // 3. Create LLM Node (always create it, even if no model is selected yet)
  const llmId = generateNodeId('llm');
  nodes.push({
    id: llmId,
    type: 'llm',
    position: { x: 100, y: yPosition },
    data: {
      modelConfigRef: formData.modelName || '',
      modelName: formData.modelName || '',
      provider: formData.modelName ? extractProvider(formData.modelName) : '',
      stream: formData.stream !== false,
    } as LLMNodeData,
  });

  // Connect previous node to llm
  const prevNodeForLlm = systemPromptId || basicInfoId;
  edges.push({
    id: `edge-${prevNodeForLlm}-${llmId}`,
    source: prevNodeForLlm,
    target: llmId,
    animated: false,
  });

  yPosition += ySpacing;

  // 4. Create Tool Nodes (if tools exist)
  let lastToolId: string | null = null;
  if (formData.tools && formData.tools.length > 0) {
    const toolId = generateNodeId('tool');
    nodes.push({
      id: toolId,
      type: 'tool',
      position: { x: 100, y: yPosition },
      data: {
        serverRef: '',
        tools: formData.tools,
      } as ToolNodeData,
    });

    lastToolId = toolId;

    // Connect previous node to tool
    const prevNodeForTool = llmId || systemPromptId || basicInfoId;
    edges.push({
      id: `edge-${prevNodeForTool}-${toolId}`,
      source: prevNodeForTool,
      target: toolId,
      animated: false,
    });

    yPosition += ySpacing;
  }

  // 5. Create Output Node (always create one)
  const outputId = generateNodeId('output');
  nodes.push({
    id: outputId,
    type: 'output',
    position: { x: 100, y: yPosition },
    data: {
      format: 'json',
      streaming: formData.stream !== false,
    } as OutputNodeData,
  });

  // Connect to output node
  const prevNodeForOutput = lastToolId || llmId || systemPromptId || basicInfoId;
  edges.push({
    id: `edge-${prevNodeForOutput}-${outputId}`,
    source: prevNodeForOutput,
    target: outputId,
    animated: false,
  });

  return { nodes, edges };
}

/**
 * Converts Visual Graph to AgentFormData
 * @param nodes All nodes in the graph
 * @param edges All edges in the graph
 * @param basicInfo Basic agent information
 * @returns AgentFormData object
 */
export function convertGraphToAgentData(
  nodes: VisualNode[],
  edges: VisualEdge[],
  basicInfo: { name: string; namespace: string; description: string }
): AgentFormData {
  const formData: AgentFormData = {
    name: basicInfo.name,
    namespace: basicInfo.namespace,
    description: basicInfo.description,
    type: 'Declarative',
    tools: [],
    stream: true,
  };

  // Extract data from each node type
  nodes.forEach(node => {
    switch (node.type) {
      case 'basic-info':
        const basicData = node.data as BasicInfoNodeData;
        formData.name = basicData.name || formData.name;
        formData.namespace = basicData.namespace || formData.namespace;
        formData.description = basicData.description || formData.description;
        formData.type = basicData.type || 'Declarative';
        break;

      case 'system-prompt':
        const promptData = node.data as SystemPromptNodeData;
        formData.systemPrompt = promptData.systemPrompt || '';
        break;

      case 'llm':
        const llmData = node.data as LLMNodeData;
        formData.modelName = llmData.modelConfigRef || llmData.modelName;
        formData.stream = llmData.stream !== false;
        break;

      case 'tool':
        const toolData = node.data as ToolNodeData;
        if (toolData.tools && Array.isArray(toolData.tools)) {
          formData.tools = [...formData.tools, ...toolData.tools];
        }
        break;

      case 'output':
        const outputData = node.data as OutputNodeData;
        if (outputData.streaming !== undefined) {
          formData.stream = outputData.streaming;
        }
        break;
    }
  });

  return formData;
}

/**
 * Helper function to extract provider from model name
 * @param modelName Full model name or ref
 * @returns Provider name
 */
function extractProvider(modelName: string): string {
  if (!modelName) return '';

  // If it's a ref like "default/gpt-4", extract just the model name
  const parts = modelName.split('/');
  const actualModelName = parts.length > 1 ? parts[parts.length - 1] : modelName;

  // Determine provider based on model name patterns
  if (actualModelName.startsWith('gpt-') || actualModelName.startsWith('o1-')) {
    return 'OpenAI';
  } else if (actualModelName.startsWith('claude-')) {
    return 'Anthropic';
  } else if (actualModelName.includes('gemini')) {
    return 'Gemini';
  } else if (actualModelName.includes('llama') || actualModelName.includes('mistral')) {
    return 'Ollama';
  }

  return 'OpenAI'; // Default fallback
}

/**
 * Merge node data from multiple nodes of the same type
 * @param nodes Array of nodes to merge
 * @returns Merged data
 */
export function mergeNodesByType(nodes: VisualNode[], nodeType: string): Record<string, unknown> {
  const matchingNodes = nodes.filter(node => node.type === nodeType);
  
  if (matchingNodes.length === 0) {
    return {};
  }

  // For most node types, just use the first one
  if (matchingNodes.length === 1) {
    return matchingNodes[0].data;
  }

  // For tool nodes, merge all tools together
  if (nodeType === 'tool') {
    const allTools: unknown[] = [];
    matchingNodes.forEach(node => {
      const toolData = node.data as ToolNodeData;
      if (toolData.tools && Array.isArray(toolData.tools)) {
        allTools.push(...toolData.tools);
      }
    });
    return { tools: allTools };
  }

  // For other types, use the first node
  return matchingNodes[0].data;
}

