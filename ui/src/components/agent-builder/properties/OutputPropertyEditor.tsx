"use client";

import * as React from "react";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

interface OutputPropertyEditorProps {
  data: {
    format: 'text' | 'json' | 'markdown';
    template?: string;
  };
  onUpdate: (data: Record<string, unknown>) => void;
}

export function OutputPropertyEditor({ data, onUpdate }: OutputPropertyEditorProps) {
  const nodeData = data as OutputPropertyEditorProps['data'];
  
  const handleChange = (field: string, value: unknown) => {
    onUpdate({ ...nodeData, [field]: value });
  };

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="format" className="text-xs">Output Format</Label>
        <Select value={nodeData.format} onValueChange={(value) => handleChange('format', value)}>
          <SelectTrigger className="text-sm">
            <SelectValue placeholder="Select format" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="text">Text</SelectItem>
            <SelectItem value="json">JSON</SelectItem>
            <SelectItem value="markdown">Markdown</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div>
        <Label htmlFor="template" className="text-xs">Output Template</Label>
        <Textarea
          id="template"
          value={nodeData.template || ''}
          onChange={(e) => handleChange('template', e.target.value)}
          placeholder="Enter output template..."
          className="text-sm min-h-[100px]"
        />
      </div>
    </div>
  );
}
