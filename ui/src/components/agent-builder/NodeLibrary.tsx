"use client";

import * as React from "react";
import { Info, Brain, FileText, Wrench, Send } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import type { NodeLibraryProps, NodeTypeDefinition, MVP_NODE_TYPES } from "@/types";

const NODE_TYPES: NodeTypeDefinition[] = [
  { 
    type: 'basic-info', 
    label: 'Basic Info', 
    icon: Info, 
    color: 'gray',
    description: 'Agent metadata and configuration',
    category: 'core'
  },
  { 
    type: 'llm', 
    label: 'LLM Model', 
    icon: Brain, 
    color: 'blue',
    description: 'Configure language model and parameters',
    category: 'core'
  },
  { 
    type: 'system-prompt', 
    label: 'System Prompt', 
    icon: FileText, 
    color: 'green',
    description: 'Define agent behavior and instructions',
    category: 'core'
  },
  { 
    type: 'tool', 
    label: 'Tool', 
    icon: Wrench, 
    color: 'orange',
    description: 'Add tools and capabilities',
    category: 'core'
  },
  { 
    type: 'output', 
    label: 'Output', 
    icon: Send, 
    color: 'red',
    description: 'Format and structure responses',
    category: 'core'
  },
];

export function NodeLibrary({ onNodeAdd, availableTypes }: NodeLibraryProps) {
  // Filter nodes based on available types (MVP support)
  const filteredNodeTypes = availableTypes 
    ? NODE_TYPES.filter(nodeType => availableTypes.includes(nodeType.type))
    : NODE_TYPES;

  const handleNodeClick = (nodeType: NodeTypeDefinition) => {
    // Add node at a default position (will be adjusted by canvas)
    onNodeAdd(nodeType.type);
  };

  const handleKeyDown = (event: React.KeyboardEvent, nodeType: NodeTypeDefinition) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      handleNodeClick(nodeType);
    }
  };

  return (
    <div className="w-64 border-r bg-muted/50 flex-shrink-0 flex flex-col h-full overflow-hidden">
      <div className="p-4 flex-1 overflow-y-auto">
        <h3 className="font-semibold mb-4 text-sm">Node Library</h3>
        <div className="space-y-2">
        {filteredNodeTypes.map((nodeType) => {
          const IconComponent = nodeType.icon;
          return (
            <Card 
              key={nodeType.type}
              className="cursor-pointer hover:shadow-md transition-all duration-200 hover:scale-105 hover:border-primary/50 focus:outline-none focus:ring-2 focus:ring-primary/50"
              onClick={() => handleNodeClick(nodeType)}
              onKeyDown={(e) => handleKeyDown(e, nodeType)}
              tabIndex={0}
              role="button"
              aria-label={`Add ${nodeType.label} node`}
            >
              <CardContent className="p-3">
                <div className="flex items-center gap-2 mb-1">
                  <IconComponent className={`w-4 h-4 text-${nodeType.color}-500`} />
                  <span className="text-sm font-medium">{nodeType.label}</span>
                </div>
                <p className="text-xs text-muted-foreground">
                  {nodeType.description}
                </p>
              </CardContent>
            </Card>
          );
        })}
        </div>
        
        <div className="mt-6 p-3 bg-muted rounded-lg">
          <h4 className="text-xs font-medium mb-2">Instructions</h4>
          <ul className="text-xs text-muted-foreground space-y-1">
            <li>• Click nodes to add them to canvas</li>
            <li>• Connect nodes to create flow</li>
            <li>• Select nodes to configure properties</li>
          </ul>
        </div>
      </div>
    </div>
  );
}
