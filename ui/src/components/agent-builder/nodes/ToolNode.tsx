"use client";

import * as React from "react";
import { Handle, Position, NodeProps } from "@xyflow/react";
import { Wrench } from "lucide-react";
import type { ToolNodeData } from "@/types";

export function ToolNode({ data, selected }: NodeProps) {
  const nodeData = data as unknown as ToolNodeData;
  
  // Safety check to prevent rendering objects directly
  if (!nodeData || typeof nodeData !== 'object') {
    return null;
  }
  
  const toolCount = nodeData.tools?.length || 0;
  const toolSummary = toolCount > 0 
    ? `${toolCount} tool${toolCount !== 1 ? 's' : ''}`
    : 'No tools';

  // Get first few tool names for display
  const getToolNames = () => {
    if (!nodeData.tools || nodeData.tools.length === 0) return null;
    
    const names: string[] = [];
    nodeData.tools.slice(0, 2).forEach(tool => {
      if (tool.type === 'McpServer' && tool.mcpServer) {
        if (tool.mcpServer.toolNames && tool.mcpServer.toolNames.length > 0) {
          names.push(tool.mcpServer.toolNames[0]);
        }
      } else if (tool.type === 'Agent' && tool.agent) {
        names.push(tool.agent.name);
      }
    });
    
    return names.length > 0 ? names.join(', ') : null;
  };

  const toolNames = getToolNames();
  
  return (
    <div className={`px-4 py-2 shadow-md rounded-md bg-white border-2 w-[200px] transition-all duration-200 ${
      selected ? 'border-orange-500 shadow-lg scale-105' : 'border-gray-300 hover:border-orange-300 hover:shadow-lg'
    }`}>
      <div className="flex items-center gap-2">
        <Wrench className="w-4 h-4 text-orange-500 flex-shrink-0" />
        <div className="flex-1 min-w-0">
          <div className="font-bold text-sm text-gray-900">Tools</div>
          <div className="text-xs text-gray-600 truncate">
            {toolSummary}
          </div>
          {toolNames && (
            <div className="text-xs text-gray-500 truncate" title={toolNames}>
              {toolNames}
            </div>
          )}
        </div>
      </div>
      <Handle 
        type="target" 
        position={Position.Top} 
        className="w-3 h-3 bg-orange-500 border-2 border-white"
        style={{ top: -6 }}
      />
      <Handle 
        type="source" 
        position={Position.Bottom} 
        className="w-3 h-3 bg-orange-500 border-2 border-white"
        style={{ bottom: -6 }}
      />
    </div>
  );
}
