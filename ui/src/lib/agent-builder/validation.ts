import type { 
  VisualNode, 
  VisualEdge, 
  VisualBuilderValidationResult,
  LLMNodeData,
  SystemPromptNodeData,
  BasicInfoNodeData 
} from "@/types";
import { detectCycles, findOrphanedNodes, getNodesByType } from "./utils";
import { isResourceNameValid } from "@/lib/utils";

/**
 * Validates complete visual graph
 * @param nodes All nodes in the graph
 * @param edges All edges in the graph
 * @param basicInfo Basic agent information
 * @returns Validation result with errors and warnings
 */
export function validateVisualGraph(
  nodes: VisualNode[],
  edges: VisualEdge[],
  basicInfo: { name: string; namespace: string; description: string }
): VisualBuilderValidationResult {
  const errors: Record<string, string> = {};
  const warnings: string[] = [];

  // If no nodes, return early with basic validation
  if (!nodes || nodes.length === 0) {
    errors.general = "No nodes added to the graph. Add at least one node to get started.";
    return { isValid: false, errors, warnings };
  }

  // Validate basic info
  const basicInfoErrors = validateBasicInfo(basicInfo);
  Object.assign(errors, basicInfoErrors);

  // Validate required nodes
  const requiredNodeErrors = validateRequiredNodes(nodes);
  Object.assign(errors, requiredNodeErrors);

  // Validate individual nodes
  const nodeErrors = validateNodes(nodes);
  Object.assign(errors, nodeErrors);

  // Validate graph structure
  const structureWarnings = validateGraphStructure(nodes, edges);
  warnings.push(...structureWarnings);

  // Check for cycles
  if (detectCycles(nodes, edges)) {
    warnings.push("Graph contains cycles. This may cause unexpected behavior.");
  }

  // Check for orphaned nodes (except basic-info which can be standalone)
  const orphanedNodes = findOrphanedNodes(nodes, edges);
  const orphanedNonBasicNodes = orphanedNodes.filter(nodeId => {
    const node = nodes.find(n => n.id === nodeId);
    return node && node.type !== 'basic-info';
  });

  if (orphanedNonBasicNodes.length > 0) {
    warnings.push(`${orphanedNonBasicNodes.length} node(s) are not connected. Consider connecting them or removing them.`);
  }

  const isValid = Object.keys(errors).length === 0;

  return { isValid, errors, warnings };
}

/**
 * Validate basic agent information
 */
function validateBasicInfo(basicInfo: { name: string; namespace: string; description: string }): Record<string, string> {
  const errors: Record<string, string> = {};

  if (!basicInfo.name || basicInfo.name.trim() === '') {
    errors.name = "Agent name is required";
  } else if (!isResourceNameValid(basicInfo.name)) {
    errors.name = "Agent name must be a valid Kubernetes resource name (lowercase alphanumeric, hyphens, max 63 chars)";
  }

  if (!basicInfo.namespace || basicInfo.namespace.trim() === '') {
    errors.namespace = "Namespace is required";
  } else if (!isResourceNameValid(basicInfo.namespace)) {
    errors.namespace = "Namespace must be a valid Kubernetes resource name";
  }

  return errors;
}

/**
 * Validate required nodes exist
 */
function validateRequiredNodes(nodes: VisualNode[]): Record<string, string> {
  const errors: Record<string, string> = {};

  // Check for LLM node (required)
  const llmNodes = getNodesByType(nodes, 'llm');
  if (llmNodes.length === 0) {
    errors.llm = "Add an LLM node to configure the model";
  } else if (llmNodes.length > 1) {
    errors.llm = "Only one LLM model configuration is allowed";
  }

  // Check for system prompt node (required)
  const systemPromptNodes = getNodesByType(nodes, 'system-prompt');
  if (systemPromptNodes.length === 0) {
    errors.systemPrompt = "Add a System Prompt node to define agent behavior";
  }

  return errors;
}

/**
 * Validate individual nodes
 */
function validateNodes(nodes: VisualNode[]): Record<string, string> {
  const errors: Record<string, string> = {};

  nodes.forEach((node, index) => {
    if (!node.type) {
      errors[`node_${index}`] = `Node ${node.id} has no type`;
      return;
    }

    switch (node.type) {
      case 'basic-info':
        const basicInfoErrors = validateBasicInfoNode(node);
        Object.assign(errors, basicInfoErrors);
        break;

      case 'llm':
        const llmErrors = validateLLMNode(node);
        Object.assign(errors, llmErrors);
        break;

      case 'system-prompt':
        const promptErrors = validateSystemPromptNode(node);
        Object.assign(errors, promptErrors);
        break;

      case 'tool':
        // Tool validation is optional - tools can be empty
        break;

      case 'output':
        // Output validation is optional
        break;

      default:
        errors[`node_${index}`] = `Unknown node type: ${node.type}`;
    }
  });

  return errors;
}

/**
 * Validate basic info node
 */
function validateBasicInfoNode(node: VisualNode): Record<string, string> {
  const errors: Record<string, string> = {};
  const data = node.data as BasicInfoNodeData;

  // Only validate if the node actually has data fields (not just checking existence)
  if (data.name !== undefined && (!data.name || data.name.trim() === '')) {
    errors[`${node.id}_name`] = "Basic info node: Agent name is required";
  }

  if (data.namespace !== undefined && (!data.namespace || data.namespace.trim() === '')) {
    errors[`${node.id}_namespace`] = "Basic info node: Namespace is required";
  }

  return errors;
}

/**
 * Validate LLM node
 */
function validateLLMNode(node: VisualNode): Record<string, string> {
  const errors: Record<string, string> = {};
  const data = node.data as LLMNodeData;

  if (!data.modelName || data.modelName.trim() === '') {
    errors.model = "LLM node: Model must be selected";
  }

  if (!data.modelConfigRef || data.modelConfigRef.trim() === '') {
    errors[`${node.id}_modelRef`] = "LLM node: Model configuration reference is required";
  }

  return errors;
}

/**
 * Validate system prompt node
 */
function validateSystemPromptNode(node: VisualNode): Record<string, string> {
  const errors: Record<string, string> = {};
  const data = node.data as SystemPromptNodeData;

  if (!data.systemPrompt || data.systemPrompt.trim() === '') {
    errors.systemPrompt = "System prompt cannot be empty";
  }

  return errors;
}

/**
 * Validate graph structure
 */
function validateGraphStructure(
  nodes: VisualNode[],
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  edges: VisualEdge[]
): string[] {
  const warnings: string[] = [];

  // Tools are optional - no warning needed if not configured
  // Users can create agents without tools

  // Check for multiple system prompts
  const systemPromptNodes = getNodesByType(nodes, 'system-prompt');
  if (systemPromptNodes.length > 1) {
    warnings.push("Multiple system prompts detected. Only the first one will be used.");
  }

  // Check for multiple LLM nodes
  const llmNodes = getNodesByType(nodes, 'llm');
  if (llmNodes.length > 1) {
    warnings.push("Multiple LLM configurations detected. Only one is allowed.");
  }

  return warnings;
}

/**
 * Returns human-readable validation summary
 */
export function getValidationSummary(result: VisualBuilderValidationResult): string {
  if (result.isValid) {
    return "✓ Agent configuration is valid";
  }

  const errorCount = Object.keys(result.errors).length;
  const warningCount = result.warnings.length;

  let summary = `✗ Found ${errorCount} error${errorCount !== 1 ? 's' : ''}`;
  if (warningCount > 0) {
    summary += ` and ${warningCount} warning${warningCount !== 1 ? 's' : ''}`;
  }

  return summary;
}

