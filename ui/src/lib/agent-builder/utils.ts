import type { VisualNode, VisualEdge, NodeData } from "@/types";

/**
 * Generate unique node ID
 * @param nodeType Type of the node
 * @returns Unique node ID
 */
export function generateNodeId(nodeType: string): string {
  const timestamp = Date.now();
  const random = Math.floor(Math.random() * 1000);
  return `${nodeType}-${timestamp}-${random}`;
}

/**
 * Calculate position for new node using grid layout
 * @param existingNodes Existing nodes in the graph
 * @param nodeType Type of the node being added (reserved for future use)
 * @returns Position coordinates {x, y}
 */
export function calculateNodePosition(
  existingNodes: VisualNode[],
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  nodeType: string
): { x: number; y: number } {
  // Grid layout configuration
  const HORIZONTAL_SPACING = 300;
  const VERTICAL_SPACING = 200;
  const START_X = 100;
  const START_Y = 100;

  // Calculate position based on total nodes
  const totalNodes = existingNodes.length;
  const row = Math.floor(totalNodes / 3);
  const col = totalNodes % 3;

  return {
    x: START_X + (col * HORIZONTAL_SPACING),
    y: START_Y + (row * VERTICAL_SPACING),
  };
}

/**
 * Get default data for node type
 * @param nodeType Type of the node
 * @returns Default node data
 */
export function getDefaultNodeData(nodeType: string): NodeData {
  switch (nodeType) {
    case 'basic-info':
      return {
        name: 'my-agent',
        namespace: 'default',
        description: 'Agent created with visual builder',
        type: 'Declarative' as const,
      };
    case 'llm':
      return {
        modelConfigRef: '',
        modelName: '',
        provider: '',
        stream: true,
      };
    case 'system-prompt':
      return {
        systemPrompt: "You're a helpful agent, made by the kagent team.\n\n# Instructions\n- If user question is unclear, ask for clarification before running any tools\n- Always be helpful and friendly\n- If you don't know how to answer the question DO NOT make things up, tell the user \"Sorry, I don't know how to answer that\" and ask them to clarify the question further\n\n# Response format:\n- ALWAYS format your response as Markdown",
      };
    case 'tool':
      return {
        serverRef: '',
        tools: [],
      };
    case 'output':
      return {
        format: 'json',
        template: '',
        streaming: true,
      };
    default:
      return { label: nodeType };
  }
}

/**
 * Create default connected graph with Basic Info, LLM, System Prompt, and Output nodes
 * @returns Object containing default nodes and edges arrays
 */
export function createDefaultGraph(): { nodes: VisualNode[], edges: VisualEdge[] } {
  const basicInfoId = generateNodeId('basic-info');
  const llmId = generateNodeId('llm');
  const systemPromptId = generateNodeId('system-prompt');
  const outputId = generateNodeId('output');

  const nodes: VisualNode[] = [
    {
      id: basicInfoId,
      type: 'basic-info',
      position: { x: 600, y: 50 },
      data: getDefaultNodeData('basic-info'),
    },
    {
      id: llmId,
      type: 'llm',
      position: { x: 600, y: 180 },
      data: getDefaultNodeData('llm'),
    },
    {
      id: systemPromptId,
      type: 'system-prompt',
      position: { x: 600, y: 310 },
      data: getDefaultNodeData('system-prompt'),
    },
    {
      id: outputId,
      type: 'output',
      position: { x: 600, y: 440 },
      data: getDefaultNodeData('output'),
    },
  ];

  const edges: VisualEdge[] = [
    {
      id: `${basicInfoId}-${llmId}`,
      source: basicInfoId,
      target: llmId,
      type: 'smoothstep',
    },
    {
      id: `${llmId}-${systemPromptId}`,
      source: llmId,
      target: systemPromptId,
      type: 'smoothstep',
    },
    {
      id: `${systemPromptId}-${outputId}`,
      source: systemPromptId,
      target: outputId,
      type: 'smoothstep',
    },
  ];

  console.log('🔧 Creating default graph with nodes:', nodes.map(n => n.type));
  console.log('🔗 Creating edges:', edges.map(e => `${e.source} → ${e.target}`));

  return { nodes, edges };
}

/**
 * Detect cycles in graph using DFS
 * @param nodes All nodes in the graph
 * @param edges All edges in the graph
 * @returns True if cycles exist, false otherwise
 */
export function detectCycles(nodes: VisualNode[], edges: VisualEdge[]): boolean {
  const adjacencyList = new Map<string, string[]>();
  const visited = new Set<string>();
  const recursionStack = new Set<string>();

  // Build adjacency list
  nodes.forEach(node => {
    adjacencyList.set(node.id, []);
  });

  edges.forEach(edge => {
    const neighbors = adjacencyList.get(edge.source) || [];
    neighbors.push(edge.target);
    adjacencyList.set(edge.source, neighbors);
  });

  // DFS helper function
  function hasCycleDFS(nodeId: string): boolean {
    visited.add(nodeId);
    recursionStack.add(nodeId);

    const neighbors = adjacencyList.get(nodeId) || [];
    for (const neighbor of neighbors) {
      if (!visited.has(neighbor)) {
        if (hasCycleDFS(neighbor)) {
          return true;
        }
      } else if (recursionStack.has(neighbor)) {
        return true;
      }
    }

    recursionStack.delete(nodeId);
    return false;
  }

  // Check each node
  for (const node of nodes) {
    if (!visited.has(node.id)) {
      if (hasCycleDFS(node.id)) {
        return true;
      }
    }
  }

  return false;
}

/**
 * Find orphaned nodes (nodes with no connections)
 * @param nodes All nodes in the graph
 * @param edges All edges in the graph
 * @returns Array of orphaned node IDs
 */
export function findOrphanedNodes(nodes: VisualNode[], edges: VisualEdge[]): string[] {
  const connectedNodes = new Set<string>();

  // Mark all nodes that have connections
  edges.forEach(edge => {
    connectedNodes.add(edge.source);
    connectedNodes.add(edge.target);
  });

  // Find nodes that are not connected
  const orphanedNodes = nodes
    .filter(node => !connectedNodes.has(node.id))
    .map(node => node.id);

  return orphanedNodes;
}

/**
 * Get node by ID
 * @param nodes All nodes in the graph
 * @param nodeId ID of the node to find
 * @returns Node or undefined
 */
export function getNodeById(nodes: VisualNode[], nodeId: string): VisualNode | undefined {
  return nodes.find(node => node.id === nodeId);
}

/**
 * Get nodes by type
 * @param nodes All nodes in the graph
 * @param nodeType Type to filter by
 * @returns Array of nodes of the specified type
 */
export function getNodesByType(nodes: VisualNode[], nodeType: string): VisualNode[] {
  return nodes.filter(node => node.type === nodeType);
}

/**
 * Validate node data structure
 * @param nodeType Type of the node
 * @param data Node data to validate
 * @returns True if valid, false otherwise
 */
export function isValidNodeData(nodeType: string, data: NodeData): boolean {
  if (!data || typeof data !== 'object') {
    return false;
  }

  switch (nodeType) {
    case 'basic-info':
      return typeof data.name === 'string' && typeof data.namespace === 'string';
    case 'llm':
      return typeof data.modelName === 'string' && typeof data.provider === 'string';
    case 'system-prompt':
      return typeof data.systemPrompt === 'string';
    case 'tool':
      return Array.isArray(data.tools);
    default:
      return true;
  }
}

