"use client";

import React from "react";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Card, CardContent } from "@/components/ui/card";
import { Trash2, PlusCircle } from "lucide-react";
import { SubAgentRef } from "@/components/AgentsProvider";
import { useAgents } from "@/components/AgentsProvider";
import { k8sRefUtils } from "@/lib/k8sUtils";

interface WorkflowSectionProps {
  workflowType: "sequential" | "parallel" | "loop" | undefined;
  onWorkflowTypeChange: (type: "sequential" | "parallel" | "loop") => void;
  subAgents: SubAgentRef[];
  onSubAgentsChange: (subAgents: SubAgentRef[]) => void;
  maxWorkers?: number;
  onMaxWorkersChange: (maxWorkers: number) => void;
  maxIterations?: number;
  onMaxIterationsChange: (maxIterations: number) => void;
  timeout?: string;
  onTimeoutChange: (timeout: string) => void;
  disabled?: boolean;
  currentAgentName?: string;
  error?: string;
}

export function WorkflowSection({
  workflowType,
  onWorkflowTypeChange,
  subAgents,
  onSubAgentsChange,
  maxWorkers,
  onMaxWorkersChange,
  maxIterations,
  onMaxIterationsChange,
  timeout,
  onTimeoutChange,
  disabled,
  currentAgentName,
  error,
}: WorkflowSectionProps) {
  const { agents } = useAgents();

  // Filter out the current agent from available agents
  const availableAgents = agents.filter(
    (agent) => agent.agent.metadata.name !== currentAgentName
  );

  const handleAddSubAgent = () => {
    onSubAgentsChange([...subAgents, { name: "", namespace: "default" }]);
  };

  const handleRemoveSubAgent = (index: number) => {
    const newSubAgents = subAgents.filter((_, i) => i !== index);
    onSubAgentsChange(newSubAgents);
  };

  const handleSubAgentChange = (index: number, field: keyof SubAgentRef, value: string) => {
    const newSubAgents = [...subAgents];
    newSubAgents[index] = { ...newSubAgents[index], [field]: value };
    onSubAgentsChange(newSubAgents);
  };

  const handleAgentSelect = (index: number, agentRef: string) => {
    if (!agentRef) return;
    
    const { name, namespace } = k8sRefUtils.fromRef(agentRef);
    const newSubAgents = [...subAgents];
    newSubAgents[index] = { name, namespace };
    onSubAgentsChange(newSubAgents);
  };

  return (
    <div className="space-y-4">
      <div>
        <Label className="text-base mb-2 block font-bold">Workflow Type</Label>
        <p className="text-xs mb-2 block text-muted-foreground">
          Sequential executes agents in order, Parallel executes them concurrently, Loop executes iteratively.
        </p>
        <Select
          value={workflowType}
          onValueChange={(val) => onWorkflowTypeChange(val as "sequential" | "parallel" | "loop")}
          disabled={disabled}
        >
          <SelectTrigger>
            <SelectValue placeholder="Select workflow type" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="sequential">Sequential</SelectItem>
            <SelectItem value="parallel">Parallel</SelectItem>
            <SelectItem value="loop">Loop</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div>
        <Label className="text-base mb-2 block font-bold">Sub-Agents</Label>
        <p className="text-xs mb-2 block text-muted-foreground">
          Select at least 2 agents to include in this workflow. Agents will be executed in the order specified.
        </p>
        <div className="space-y-2">
          {subAgents.map((subAgent, index) => (
            <Card key={index}>
              <CardContent className="pt-4">
                <div className="flex gap-2 items-start">
                  <div className="flex-1 space-y-2">
                    <Select
                      value={subAgent.name ? k8sRefUtils.toRef(subAgent.namespace || "default", subAgent.name) : ""}
                      onValueChange={(val) => handleAgentSelect(index, val)}
                      disabled={disabled}
                    >
                      <SelectTrigger>
                        <SelectValue placeholder="Select an agent" />
                      </SelectTrigger>
                      <SelectContent>
                        {availableAgents.map((agent) => {
                          const ref = k8sRefUtils.toRef(
                            agent.agent.metadata.namespace || "default",
                            agent.agent.metadata.name
                          );
                          return (
                            <SelectItem key={ref} value={ref}>
                              {agent.agent.metadata.name} ({agent.agent.metadata.namespace})
                            </SelectItem>
                          );
                        })}
                      </SelectContent>
                    </Select>
                    <div className="grid grid-cols-2 gap-2">
                      <div>
                        <Label className="text-xs">Agent Name</Label>
                        <Input
                          value={subAgent.name}
                          onChange={(e) => handleSubAgentChange(index, "name", e.target.value)}
                          placeholder="agent-name"
                          disabled={disabled}
                          className={!subAgent.name || !subAgent.name.trim() ? "border-red-500" : ""}
                        />
                        {(!subAgent.name || !subAgent.name.trim()) && (
                          <p className="text-red-500 text-xs mt-1">Agent name is required</p>
                        )}
                      </div>
                      <div>
                        <Label className="text-xs">Namespace</Label>
                        <Input
                          value={subAgent.namespace || ""}
                          onChange={(e) => handleSubAgentChange(index, "namespace", e.target.value)}
                          placeholder="default"
                          disabled={disabled}
                        />
                      </div>
                    </div>
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleRemoveSubAgent(index)}
                    disabled={disabled || subAgents.length <= 2}
                    className="mt-1"
                  >
                    <Trash2 className="h-4 w-4 text-red-500" />
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
          <Button
            variant="outline"
            size="sm"
            onClick={handleAddSubAgent}
            disabled={disabled || subAgents.length >= 50}
            className="w-full"
          >
            <PlusCircle className="h-4 w-4 mr-2" />
            Add Sub-Agent
          </Button>
          {subAgents.length < 2 && (
            <p className="text-red-500 text-sm">At least 2 sub-agents are required</p>
          )}
          {subAgents.length >= 50 && (
            <p className="text-orange-500 text-sm">Maximum 50 sub-agents allowed</p>
          )}
          {error && <p className="text-red-500 text-sm">{error}</p>}
        </div>
      </div>

      {workflowType === "parallel" && (
        <div>
          <Label className="text-sm mb-2 block">Max Workers (optional)</Label>
          <p className="text-xs mb-2 block text-muted-foreground">
            Maximum number of sub-agents executing concurrently (1-50, default: 10)
          </p>
          <Input
            type="number"
            min={1}
            max={50}
            value={maxWorkers || ""}
            onChange={(e) => onMaxWorkersChange(parseInt(e.target.value, 10))}
            placeholder="10"
            disabled={disabled}
          />
        </div>
      )}

      {workflowType === "loop" && (
        <div>
          <Label className="text-sm mb-2 block">Max Iterations</Label>
          <p className="text-xs mb-2 block text-muted-foreground">
            Maximum number of iterations for the loop workflow (required)
          </p>
          <Input
            type="number"
            min={1}
            value={maxIterations || ""}
            onChange={(e) => onMaxIterationsChange(parseInt(e.target.value, 10))}
            placeholder="10"
            disabled={disabled}
          />
        </div>
      )}

      <div>
        <Label className="text-sm mb-2 block">Timeout (optional)</Label>
        <p className="text-xs mb-2 block text-muted-foreground">
          Timeout for each sub-agent execution (e.g., &quot;5m&quot;, &quot;300s&quot;)
        </p>
        <Input
          value={timeout || ""}
          onChange={(e) => onTimeoutChange(e.target.value)}
          placeholder="5m"
          disabled={disabled}
        />
      </div>
    </div>
  );
}

