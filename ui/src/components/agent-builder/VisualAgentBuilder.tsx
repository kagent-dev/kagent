"use client";

import * as React from "react";
import { useState, useCallback, useMemo, useRef } from "react";
import { Node, NodeChange, EdgeChange, addEdge, applyNodeChanges, applyEdgeChanges } from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import { NodeLibrary } from "./NodeLibrary";
import { Canvas } from "./Canvas";
import { NodeProperties } from "./properties/NodeProperties";
import { convertFormDataToGraph, convertGraphToAgentData } from "@/lib/agent-builder/graphConverter";
import { validateVisualGraph } from "@/lib/agent-builder/validation";
import { generateNodeId, calculateNodePosition, getDefaultNodeData, createDefaultGraph } from "@/lib/agent-builder/utils";
import type { 
  AgentFormData, 
  VisualBuilderValidationResult, 
  VisualAgentBuilderProps,
  VisualNode,
  VisualEdge
} from "@/types";
import { MVP_NODE_TYPES } from "@/types";

const DEFAULT_SYSTEM_PROMPT = `You're a helpful agent, made by the kagent team.

# Instructions
    - If user question is unclear, ask for clarification before running any tools
    - Always be helpful and friendly
    - If you don't know how to answer the question DO NOT make things up, tell the user "Sorry, I don't know how to answer that" and ask them to clarify the question further
    - If you are unable to help, or something goes wrong, refer the user to https://kagent.dev for more information or support.

# Response format:
- ALWAYS format your response as Markdown
- Your response will include a summary of actions you took and an explanation of the result
- If you created any artifacts such as files or resources, you will include those in your response as well`;

export function VisualAgentBuilder({
  onValidationChange,
  onGraphDataChange,
  initialFormData,
  onCreateAgent,
  isSubmitting
}: VisualAgentBuilderProps) {
  const [nodes, setNodes] = useState<VisualNode[]>([]);
  const [edges, setEdges] = useState<VisualEdge[]>([]);
  const [selectedNode, setSelectedNode] = useState<string | null>(null);
  const [selectedNodes, setSelectedNodes] = useState<string[]>([]);
  const [selectedEdges, setSelectedEdges] = useState<string[]>([]);
  const [validationResult, setValidationResult] = useState<VisualBuilderValidationResult | null>(null);
  const [isInitialized, setIsInitialized] = useState(false);
  const initializationRef = useRef(false);

  // Initialize graph from form data if provided - only run once
  React.useEffect(() => {
    if (!initializationRef.current) {
      // Check if initialFormData has meaningful content (not just empty strings)
      const hasMeaningfulData = initialFormData && (
        initialFormData.modelName || 
        (initialFormData.tools && initialFormData.tools.length > 0)
      );

      console.log('🔍 Checking initialFormData:', {
        hasData: !!initialFormData,
        modelName: initialFormData?.modelName,
        tools: initialFormData?.tools?.length,
        systemPrompt: initialFormData?.systemPrompt?.substring(0, 50) + '...',
        hasMeaningfulData
      });

      if (hasMeaningfulData) {
        // Use existing form data to build graph
        try {
          const { nodes: initialNodes, edges: initialEdges } = convertFormDataToGraph(initialFormData as AgentFormData);
          setNodes(initialNodes);
          setEdges(initialEdges);
          initializationRef.current = true;
          setIsInitialized(true);
        } catch (error) {
          console.error("Error initializing visual builder:", error);
          // Fallback to default graph on error
          const { nodes: defaultNodes, edges: defaultEdges } = createDefaultGraph();
          setNodes(defaultNodes);
          setEdges(defaultEdges);
          initializationRef.current = true;
          setIsInitialized(true);
        }
      } else {
        // Create default graph with Basic Info, LLM, System Prompt, and Output nodes
        try {
          console.log('📊 Creating default graph (no meaningful initial data)');
          const { nodes: defaultNodes, edges: defaultEdges } = createDefaultGraph();
          console.log('✅ Default graph created:', defaultNodes.length, 'nodes,', defaultEdges.length, 'edges');
          console.log('📍 Nodes:', defaultNodes.map(n => `${n.type} (${n.id})`));
          setNodes(defaultNodes);
          setEdges(defaultEdges);
          initializationRef.current = true;
          setIsInitialized(true);
        } catch (error) {
          console.error("Error creating default graph:", error);
          initializationRef.current = true;
          setIsInitialized(true);
        }
      }
    }
  }, [initialFormData]);

  const onNodesChange = useCallback(
    (changes: NodeChange[]) => setNodes((nds) => applyNodeChanges(changes, nds) as VisualNode[]),
    [setNodes]
  );

  const onEdgesChange = useCallback(
    (changes: EdgeChange[]) => setEdges((eds) => applyEdgeChanges(changes, eds) as VisualEdge[]),
    [setEdges]
  );

  const onConnect = useCallback(
    (params: { source: string; target: string; sourceHandle?: string | null; targetHandle?: string | null }) => {
      const connection = {
        source: params.source,
        target: params.target,
        sourceHandle: params.sourceHandle ?? null,
        targetHandle: params.targetHandle ?? null,
      };
      setEdges((eds) => addEdge(connection, eds));
    },
    [setEdges]
  );

  const onNodeAdd = useCallback((nodeType: string) => {
    try {
      const newNode: Node = {
        id: generateNodeId(nodeType),
        type: nodeType,
        position: calculateNodePosition(nodes, nodeType),
        data: getDefaultNodeData(nodeType),
      };
      setNodes((nds) => [...nds, newNode as VisualNode]);
    } catch (error) {
      console.error("Error adding node:", error);
    }
  }, [nodes]);

  const onNodeUpdate = useCallback((nodeId: string, data: Record<string, unknown>) => {
    try {
      setNodes((nds) =>
        nds.map((node) =>
          node.id === nodeId ? { ...node, data: { ...node.data, ...data } } : node
        )
      );
    } catch (error) {
      console.error("Error updating node:", error);
    }
  }, []);

  // Memoize basic info to prevent unnecessary re-renders
  const basicInfo = useMemo(() => ({
    name: initialFormData?.name || "visual-agent",
    namespace: initialFormData?.namespace || "default",
    description: initialFormData?.description || "Agent created with visual builder",
  }), [initialFormData?.name, initialFormData?.namespace, initialFormData?.description]);

  // Validate graph whenever nodes or edges change (debounced)
  React.useEffect(() => {
    if (!isInitialized) return;

    const timeoutId = setTimeout(() => {
      try {
        const result = validateVisualGraph(nodes, edges, basicInfo);
        setValidationResult(result);
        onValidationChange(result.errors);

        // Convert graph to agent data and notify parent
        if (onGraphDataChange && nodes.length > 0) {
          try {
            const agentData = convertGraphToAgentData(nodes, edges, basicInfo);
            onGraphDataChange(agentData);
          } catch (error) {
            console.error("Error converting graph to agent data:", error);
            // Don't call onGraphDataChange if there's an error
          }
        }
      } catch (error) {
        console.error("Error in validation:", error);
        // Set a safe validation result
        const safeResult = {
          isValid: false,
          errors: { general: "Validation error occurred" },
          warnings: []
        };
        setValidationResult(safeResult);
        onValidationChange(safeResult.errors);
      }
    }, 300); // Debounce validation by 300ms

    return () => clearTimeout(timeoutId);
  }, [nodes, edges, basicInfo, onValidationChange, onGraphDataChange, isInitialized]);

  const onNodeSelect = useCallback((nodeId: string | null) => {
    setSelectedNode(nodeId);
  }, []);

  const onNodesSelect = useCallback((nodeIds: string[]) => {
    setSelectedNodes(nodeIds);
  }, []);

  const onEdgeSelect = useCallback((edgeIds: string[]) => {
    setSelectedEdges(edgeIds);
  }, []);

  // Handle deletion of selected nodes and edges
  const handleDelete = useCallback(() => {
    // Delete multiple selected nodes
    if (selectedNodes.length > 0) {
      setNodes((nds) => nds.filter((node) => !selectedNodes.includes(node.id)));
      setEdges((eds) => eds.filter(
        (edge) => !selectedNodes.includes(edge.source) && !selectedNodes.includes(edge.target)
      ));
      setSelectedNodes([]);
      setSelectedNode(null);
    }
    // Delete single selected node
    else if (selectedNode) {
      setNodes((nds) => nds.filter((node) => node.id !== selectedNode));
      setEdges((eds) => eds.filter(
        (edge) => edge.source !== selectedNode && edge.target !== selectedNode
      ));
      setSelectedNode(null);
    }
    // Delete selected edges
    else if (selectedEdges.length > 0) {
      setEdges((eds) => eds.filter((edge) => !selectedEdges.includes(edge.id)));
      setSelectedEdges([]);
    }
  }, [selectedNode, selectedNodes, selectedEdges]);

  // Keyboard event handler for Delete/Backspace keys
  React.useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      // Check if Delete or Backspace key is pressed
      if (event.key === 'Delete' || event.key === 'Backspace') {
        // Prevent default backspace navigation
        const target = event.target as HTMLElement;
        const isInputField = target.tagName === 'INPUT' || 
                            target.tagName === 'TEXTAREA' || 
                            target.isContentEditable;
        
        // Only delete if not typing in an input field
        if (!isInputField) {
          event.preventDefault();
          handleDelete();
        }
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [handleDelete]);

  // Safety check to prevent React errors
  if (!isInitialized) {
    return (
      <div className="space-y-4">
        <div className="p-4 border rounded-lg bg-yellow-50 border-yellow-200 text-yellow-800">
          <div className="text-sm font-medium">Initializing Visual Builder...</div>
        </div>
      </div>
    );
  }

  if (!nodes || !Array.isArray(nodes)) {
    return (
      <div className="space-y-4">
        <div className="p-4 border rounded-lg bg-yellow-50 border-yellow-200 text-yellow-800">
          <div className="text-sm font-medium">Initializing Visual Builder...</div>
        </div>
      </div>
    );
  }

  // Show helpful message when no nodes are present
  if (nodes.length === 0) {
    return (
      <div className="flex-1 flex flex-col overflow-hidden">
        <div className="px-6 py-2 border-b bg-blue-50 border-blue-200 text-blue-800">
          <div className="text-xs font-medium">Welcome to the Visual Agent Builder! Start by dragging nodes from the library on the left.</div>
        </div>
        
        <div className="flex-1 flex overflow-hidden">
          <NodeLibrary onNodeAdd={onNodeAdd} />
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center text-muted-foreground">
              <div className="text-lg font-medium mb-2">No nodes yet</div>
              <div className="text-sm">Add nodes from the library to get started</div>
            </div>
          </div>
          <div className="w-80 border-l bg-muted/50 p-4 flex-shrink-0">
            <h3 className="font-semibold mb-4 text-sm">Properties</h3>
            <p className="text-sm text-muted-foreground">
              Select a node to configure its properties
            </p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex-1 flex overflow-hidden h-full min-h-0">
      <NodeLibrary 
        onNodeAdd={onNodeAdd} 
        availableTypes={MVP_NODE_TYPES} // MVP: Only show core node types
      />
      <Canvas 
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onNodeSelect={onNodeSelect}
        onNodesSelect={onNodesSelect}
        onEdgeSelect={onEdgeSelect}
        onDelete={handleDelete}
      />
      <NodeProperties 
        selectedNode={selectedNode}
        nodes={nodes}
        onNodeUpdate={onNodeUpdate}
        validationResult={validationResult}
        onCreateAgent={onCreateAgent}
        isSubmitting={isSubmitting}
      />
    </div>
  );
}

