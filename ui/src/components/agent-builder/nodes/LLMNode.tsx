"use client";

import * as React from "react";
import { Handle, Position, NodeProps } from "@xyflow/react";
import { Brain } from "lucide-react";
import type { LLMNodeData } from "@/types";

export function LLMNode({ data, selected }: NodeProps) {
  const nodeData = data as unknown as LLMNodeData;
  
  // Safety check to prevent rendering objects directly
  if (!nodeData || typeof nodeData !== 'object') {
    return null;
  }
  
  return (
    <div className={`px-4 py-2 shadow-md rounded-md bg-white border-2 w-[200px] transition-all duration-200 ${
      selected ? 'border-blue-500 shadow-lg scale-105' : 'border-gray-300 hover:border-blue-300 hover:shadow-lg'
    }`}>
      <div className="flex items-center gap-2">
        <Brain className="w-4 h-4 text-blue-500 flex-shrink-0" />
        <div className="flex-1 min-w-0">
          <div className="font-bold text-sm text-gray-900 truncate">{String(nodeData.modelName || 'Select Model')}</div>
          <div className="text-xs text-gray-600 truncate">{String(nodeData.provider || 'No provider')}</div>
        </div>
      </div>
      <Handle 
        type="target" 
        position={Position.Top} 
        className="w-3 h-3 bg-blue-500 border-2 border-white"
        style={{ top: -6 }}
      />
      <Handle 
        type="source" 
        position={Position.Bottom} 
        className="w-3 h-3 bg-blue-500 border-2 border-white"
        style={{ bottom: -6 }}
      />
    </div>
  );
}
