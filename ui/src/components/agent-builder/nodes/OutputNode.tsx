"use client";

import * as React from "react";
import { Handle, Position, NodeProps } from "@xyflow/react";
import { Send } from "lucide-react";
import type { OutputNodeData } from "@/types";

export function OutputNode({ data, selected }: NodeProps) {
  const nodeData = data as unknown as OutputNodeData;
  
  // Safety check to prevent rendering objects directly
  if (!nodeData || typeof nodeData !== 'object') {
    return null;
  }
  
  const formatDisplay = nodeData.format || 'json';
  const streamingText = nodeData.streaming ? 'Streaming' : 'Non-streaming';
  
  return (
    <div className={`px-4 py-2 shadow-md rounded-md bg-white border-2 w-[180px] transition-all duration-200 ${
      selected ? 'border-red-500 shadow-lg scale-105' : 'border-gray-300 hover:border-red-300 hover:shadow-lg'
    }`}>
      <div className="flex items-center gap-2 mb-1">
        <Send className="w-4 h-4 text-red-500 flex-shrink-0" />
        <div className="font-bold text-sm text-gray-900 truncate">Output</div>
      </div>
      <div className="text-xs text-gray-600 truncate">
        <span className="capitalize">{formatDisplay}</span>
        {nodeData.streaming !== undefined && (
          <span className="text-gray-500"> • {streamingText}</span>
        )}
      </div>
      <Handle 
        type="target" 
        position={Position.Top} 
        className="w-3 h-3 bg-red-500 border-2 border-white"
        style={{ top: -6 }}
      />
    </div>
  );
}
