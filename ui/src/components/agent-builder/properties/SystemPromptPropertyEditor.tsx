"use client";

import * as React from "react";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";

interface SystemPromptPropertyEditorProps {
  data: {
    systemPrompt: string;
  };
  onUpdate: (data: Record<string, unknown>) => void;
}

export function SystemPromptPropertyEditor({ data, onUpdate }: SystemPromptPropertyEditorProps) {
  const nodeData = data as SystemPromptPropertyEditorProps['data'];
  
  const handleChange = (field: string, value: unknown) => {
    onUpdate({ ...nodeData, [field]: value });
  };

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="systemPrompt" className="text-xs">System Prompt</Label>
        <Textarea
          id="systemPrompt"
          value={nodeData.systemPrompt}
          onChange={(e) => handleChange('systemPrompt', e.target.value)}
          placeholder="Enter system prompt..."
          className="text-sm min-h-[120px]"
        />
      </div>
    </div>
  );
}
