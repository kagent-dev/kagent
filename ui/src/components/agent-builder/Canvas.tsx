"use client";

import * as React from "react";
import { ReactFlow, ConnectionLineType, Background, Controls, MiniMap } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";

import { BasicInfoNode } from "./nodes/BasicInfoNode";
import { LLMNode } from "./nodes/LLMNode";
import { SystemPromptNode } from "./nodes/SystemPromptNode";
import { ToolNode } from "./nodes/ToolNode";
import { OutputNode } from "./nodes/OutputNode";
import type { CanvasProps } from "@/types";

// Add custom styles for React Flow controls
const controlsStyle = `
  .react-flow__controls button {
    background: white !important;
    border: 1px solid #d1d5db !important;
    color: #1f2937 !important;
  }
  .react-flow__controls button svg {
    fill: #1f2937 !important;
  }
  .react-flow__controls button:hover {
    background: #f3f4f6 !important;
  }
`;

const nodeTypes = {
  'basic-info': BasicInfoNode,
  llm: LLMNode,
  'system-prompt': SystemPromptNode,
  tool: ToolNode,
  output: OutputNode,
} as const;

export function Canvas({ 
  nodes, 
  edges, 
  onNodesChange, 
  onEdgesChange, 
  onConnect,
  onNodeSelect,
  onNodesSelect,
  onEdgeSelect,
  onDelete 
}: CanvasProps) {
  const [hasSelection, setHasSelection] = React.useState(false);
  const prevSelectionRef = React.useRef<{ nodeIds: string[], edgeIds: string[] }>({ nodeIds: [], edgeIds: [] });

  const handleSelectionChange = React.useCallback((selection: { nodes: Array<{ id: string }>, edges: Array<{ id: string }> }) => {
    const selectedNodesList = selection.nodes || [];
    const selectedEdgesList = selection.edges || [];
    
    const currentNodeIds = selectedNodesList.map(n => n.id).sort();
    const currentEdgeIds = selectedEdgesList.map(e => e.id).sort();
    
    const prevNodeIds = prevSelectionRef.current.nodeIds;
    const prevEdgeIds = prevSelectionRef.current.edgeIds;
    
    // Check if selection has actually changed
    const nodesChanged = currentNodeIds.length !== prevNodeIds.length || 
                         currentNodeIds.some((id, idx) => id !== prevNodeIds[idx]);
    const edgesChanged = currentEdgeIds.length !== prevEdgeIds.length || 
                         currentEdgeIds.some((id, idx) => id !== prevEdgeIds[idx]);
    
    if (!nodesChanged && !edgesChanged) {
      return; // No change, skip update
    }
    
    // Update ref with current selection
    prevSelectionRef.current = { nodeIds: currentNodeIds, edgeIds: currentEdgeIds };
    
    // Update hasSelection state
    setHasSelection(currentNodeIds.length > 0 || currentEdgeIds.length > 0);
    
    // Call parent callbacks only if something changed
    if (currentNodeIds.length > 1) {
      // Multiple nodes selected
      onNodesSelect(currentNodeIds);
      onNodeSelect(null);
      onEdgeSelect([]);
    } else if (currentNodeIds.length === 1) {
      // Single node selected
      onNodeSelect(currentNodeIds[0]);
      onNodesSelect([]);
      onEdgeSelect([]);
    } else if (currentEdgeIds.length > 0) {
      // Edges selected
      onEdgeSelect(currentEdgeIds);
      onNodeSelect(null);
      onNodesSelect([]);
    } else {
      // Nothing selected
      onNodeSelect(null);
      onNodesSelect([]);
      onEdgeSelect([]);
    }
  }, [onNodeSelect, onNodesSelect, onEdgeSelect]);

  // Safety check to prevent React errors
  if (!nodes || !Array.isArray(nodes) || !edges || !Array.isArray(edges)) {
    return (
      <div className="flex-1 relative flex items-center justify-center bg-background">
        <div className="text-muted-foreground">Loading canvas...</div>
      </div>
    );
  }

  return (
    <div className="flex-1 relative h-full min-h-0">
      <style dangerouslySetInnerHTML={{ __html: controlsStyle }} />
      
      {/* Floating Delete Button */}
      {hasSelection && (
        <div className="absolute top-4 right-4 z-10">
          <Button
            onClick={onDelete}
            variant="destructive"
            size="sm"
            className="shadow-lg"
          >
            <Trash2 className="w-4 h-4 mr-2" />
            Delete
          </Button>
        </div>
      )}

      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onSelectionChange={handleSelectionChange}
        nodeTypes={nodeTypes}
        connectionLineType={ConnectionLineType.SmoothStep}
        fitView
        className="bg-background"
        defaultViewport={{ x: 0, y: 0, zoom: 1 }}
        minZoom={0.1}
        maxZoom={4}
        attributionPosition="bottom-left"
        deleteKeyCode={null} // Disable default delete handling (we handle it in parent)
        multiSelectionKeyCode="Control"
      >
        <Background color="#e5e7eb" gap={20} size={1} />
        <Controls />
        <MiniMap 
          style={{ 
            backgroundColor: '#1a1d23',
            border: '1px solid #374151',
            borderRadius: '8px',
            opacity: 0.8
          }}
          maskColor="rgba(0, 0, 0, 0.5)"
          nodeColor={(node) => {
            switch (node.type) {
              case 'basic-info': return '#6b7280';
              case 'llm': return '#3b82f6';
              case 'system-prompt': return '#10b981';
              case 'tool': return '#f97316';
              case 'output': return '#ef4444';
              default: return '#6b7280';
            }
          }}
          nodeStrokeWidth={3}
          nodeBorderRadius={2}
        />
      </ReactFlow>
    </div>
  );
}
