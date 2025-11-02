"use client";

import * as React from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import type { BasicInfoNodeData } from "@/types";

interface BasicInfoPropertyEditorProps {
  data: BasicInfoNodeData;
  onUpdate: (data: Record<string, unknown>) => void;
}

export function BasicInfoPropertyEditor({ data, onUpdate }: BasicInfoPropertyEditorProps) {
  const nodeData = data as BasicInfoPropertyEditorProps['data'];
  
  const handleChange = (field: string, value: unknown) => {
    onUpdate({ ...nodeData, [field]: value });
  };

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="name" className="text-xs mb-2 block">Agent Name</Label>
        <Input
          id="name"
          value={nodeData.name}
          onChange={(e) => handleChange('name', e.target.value)}
          placeholder="Enter agent name..."
          className="text-sm h-9"
        />
        <p className="text-xs text-muted-foreground mt-1">
          Must be a valid Kubernetes resource name
        </p>
      </div>

      <div>
        <Label htmlFor="namespace" className="text-xs mb-2 block">Namespace</Label>
        <NamespaceCombobox
          value={nodeData.namespace}
          onValueChange={(value) => handleChange('namespace', value)}
          placeholder="Select namespace..."
        />
        <p className="text-xs text-muted-foreground mt-1">
          The Kubernetes namespace for this agent
        </p>
      </div>

      <div>
        <Label htmlFor="type" className="text-xs mb-2 block">Agent Type</Label>
        <Select
          value={nodeData.type || 'Declarative'}
          onValueChange={(value) => handleChange('type', value)}
        >
          <SelectTrigger className="text-sm h-9">
            <SelectValue placeholder="Select agent type..." />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="Declarative">Declarative</SelectItem>
            <SelectItem value="BYO">BYO (Bring Your Own)</SelectItem>
          </SelectContent>
        </Select>
        <p className="text-xs text-muted-foreground mt-1">
          {nodeData.type === 'BYO' 
            ? 'Bring your own containerized agent'
            : 'Use a model-driven declarative agent'}
        </p>
      </div>

      <div>
        <Label htmlFor="description" className="text-xs mb-2 block">Description</Label>
        <Textarea
          id="description"
          value={nodeData.description}
          onChange={(e) => handleChange('description', e.target.value)}
          placeholder="Describe your agent..."
          className="text-sm min-h-[80px]"
        />
        <p className="text-xs text-muted-foreground mt-1">
          Optional description for reference only
        </p>
      </div>
    </div>
  );
}
