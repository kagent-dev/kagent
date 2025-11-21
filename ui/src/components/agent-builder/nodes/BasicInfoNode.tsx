"use client";

import * as React from "react";
import { Handle, Position, NodeProps } from "@xyflow/react";
import { Info } from "lucide-react";
import type { BasicInfoNodeData } from "@/types";

export function BasicInfoNode({ data, selected }: NodeProps) {
  const nodeData = data as unknown as BasicInfoNodeData;
  
  // Safety check to prevent rendering objects directly
  if (!nodeData || typeof nodeData !== 'object') {
    return null;
  }
  
  return (
    <div className={`px-4 py-2 shadow-md rounded-md bg-white border-2 w-[200px] transition-all duration-200 ${
      selected ? 'border-gray-500 shadow-lg scale-105' : 'border-gray-300 hover:border-gray-400 hover:shadow-lg'
    }`}>
      <div className="flex items-center gap-2">
        <Info className="w-4 h-4 text-gray-500 flex-shrink-0" />
        <div className="flex-1 min-w-0">
          <div className="font-bold text-sm text-gray-900 truncate">{nodeData.name || 'Unnamed Agent'}</div>
          <div className="text-xs text-gray-600 truncate">{nodeData.namespace || 'default'}</div>
          {nodeData.type && (
            <div className="text-xs text-gray-500">
              <span className="font-medium">Type:</span> {nodeData.type}
            </div>
          )}
          {nodeData.description && (
            <div className="text-xs text-gray-500 truncate" title={nodeData.description}>
              {nodeData.description}
            </div>
          )}
        </div>
      </div>
      <Handle 
        type="source" 
        position={Position.Bottom} 
        className="w-3 h-3 bg-gray-500 border-2 border-white"
        style={{ bottom: -6 }}
      />
    </div>
  );
}
