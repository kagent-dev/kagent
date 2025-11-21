"use client";

import * as React from "react";
import { BasicInfoPropertyEditor } from "./BasicInfoPropertyEditor";
import { LLMPropertyEditor } from "./LLMPropertyEditor";
import { SystemPromptPropertyEditor } from "./SystemPromptPropertyEditor";
import { ToolPropertyEditor } from "./ToolPropertyEditor";
import { OutputPropertyEditor } from "./OutputPropertyEditor";
import type { 
  BasicInfoNodeData,
  LLMNodeData, 
  SystemPromptNodeData, 
  ToolNodeData, 
  OutputNodeData,
  VisualNode
} from "@/types";

interface PropertyEditorProps {
  nodeType: string;
  data: Record<string, unknown>;
  onUpdate: (data: Record<string, unknown>) => void;
  nodes?: VisualNode[];
}

export function PropertyEditor({ nodeType, data, onUpdate, nodes }: PropertyEditorProps) {
  // Find the basic-info node to get the agent namespace
  const basicInfoNode = nodes?.find(n => n.type === 'basic-info');
  const agentNamespace = basicInfoNode?.data?.namespace as string | undefined;

  switch (nodeType) {
    case 'basic-info':
      return <BasicInfoPropertyEditor data={data as unknown as BasicInfoNodeData} onUpdate={onUpdate} />;
    case 'llm':
      return <LLMPropertyEditor data={data as unknown as LLMNodeData} onUpdate={onUpdate} agentNamespace={agentNamespace} />;
    case 'system-prompt':
      return <SystemPromptPropertyEditor data={data as unknown as SystemPromptNodeData} onUpdate={onUpdate} />;
    case 'tool':
      return <ToolPropertyEditor data={data as unknown as ToolNodeData} onUpdate={onUpdate} />;
    case 'output':
      return <OutputPropertyEditor data={data as unknown as OutputNodeData} onUpdate={onUpdate} />;
    default:
      return (
        <div className="text-sm text-muted-foreground">
          No properties available for this node type
        </div>
      );
  }
}
