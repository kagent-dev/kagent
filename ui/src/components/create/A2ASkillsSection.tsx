import React from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { PlusCircle, Trash2, ChevronDown, ChevronUp } from "lucide-react";
import type { AgentSkill } from "@/types";

export const EMPTY_A2A_SKILL: AgentSkill = {
  id: "",
  name: "",
  description: "",
  tags: [],
  examples: [],
  inputModes: ["text"],
  outputModes: ["text"],
};

interface A2ASkillsSectionProps {
  skills: AgentSkill[];
  onChange: (skills: AgentSkill[]) => void;
  disabled?: boolean;
  error?: string;
}

export function A2ASkillsSection({ skills, onChange, disabled, error }: A2ASkillsSectionProps) {
  const [expandedIndex, setExpandedIndex] = React.useState<number | null>(skills.length > 0 ? 0 : null);

  const updateSkill = (index: number, updates: Partial<AgentSkill>) => {
    const updated = [...skills];
    updated[index] = { ...updated[index], ...updates };
    onChange(updated);
  };

  const addSkill = () => {
    onChange([...skills, { ...EMPTY_A2A_SKILL }]);
    setExpandedIndex(skills.length);
  };

  const removeSkill = (index: number) => {
    const updated = skills.filter((_, i) => i !== index);
    onChange(updated);
    if (expandedIndex === index) {
      setExpandedIndex(null);
    } else if (expandedIndex !== null && expandedIndex > index) {
      setExpandedIndex(expandedIndex - 1);
    }
  };

  const parseCommaSeparated = (value: string): string[] => {
    return value.split(",").map((s) => s.trim()).filter(Boolean);
  };

  return (
    <div className="space-y-4">
      <div>
        <Label className="text-sm mb-2 block font-semibold">A2A Skills</Label>
        <p className="text-xs mb-3 block text-muted-foreground">
          Define A2A (Agent-to-Agent) skills that this agent exposes. Each skill describes a capability
          that other agents can discover and invoke via the A2A protocol.
        </p>
      </div>

      {skills.length === 0 && (
        <p className="text-xs text-muted-foreground italic">No A2A skills configured. Click &quot;Add A2A Skill&quot; to get started.</p>
      )}

      <div className="space-y-3">
        {skills.map((skill, index) => {
          const isExpanded = expandedIndex === index;
          const skillLabel = skill.name || skill.id || `Skill ${index + 1}`;

          return (
            <div key={index} className="border rounded-md">
              <div
                className="flex items-center justify-between px-4 py-3 cursor-pointer hover:bg-muted/50"
                onClick={() => setExpandedIndex(isExpanded ? null : index)}
              >
                <div className="flex items-center gap-2">
                  {isExpanded ? (
                    <ChevronUp className="h-4 w-4 text-muted-foreground" />
                  ) : (
                    <ChevronDown className="h-4 w-4 text-muted-foreground" />
                  )}
                  <span className="text-sm font-medium">{skillLabel}</span>
                  {skill.id && skill.name && (
                    <span className="text-xs text-muted-foreground">({skill.id})</span>
                  )}
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={(e) => {
                    e.stopPropagation();
                    removeSkill(index);
                  }}
                  disabled={disabled}
                  title="Remove skill"
                >
                  <Trash2 className="h-4 w-4 text-red-500" />
                </Button>
              </div>

              {isExpanded && (
                <div className="px-4 pb-4 space-y-3 border-t">
                  <div className="grid grid-cols-2 gap-3 pt-3">
                    <div>
                      <Label className="text-xs mb-1 block">ID <span className="text-red-500">*</span></Label>
                      <Input
                        placeholder="e.g. kubernetes-deploy"
                        value={skill.id}
                        onChange={(e) => updateSkill(index, { id: e.target.value })}
                        disabled={disabled}
                        className="text-sm"
                      />
                    </div>
                    <div>
                      <Label className="text-xs mb-1 block">Name <span className="text-red-500">*</span></Label>
                      <Input
                        placeholder="e.g. Kubernetes Deployment Manager"
                        value={skill.name}
                        onChange={(e) => updateSkill(index, { name: e.target.value })}
                        disabled={disabled}
                        className="text-sm"
                      />
                    </div>
                  </div>

                  <div>
                    <Label className="text-xs mb-1 block">Description</Label>
                    <Textarea
                      placeholder="Describe what this skill does..."
                      value={skill.description || ""}
                      onChange={(e) => updateSkill(index, { description: e.target.value })}
                      disabled={disabled}
                      className="text-sm min-h-[60px]"
                    />
                  </div>

                  <div>
                    <Label className="text-xs mb-1 block">Tags (comma-separated)</Label>
                    <Input
                      placeholder="e.g. kubernetes, deploy, infrastructure"
                      value={(skill.tags || []).join(", ")}
                      onChange={(e) => updateSkill(index, { tags: parseCommaSeparated(e.target.value) })}
                      disabled={disabled}
                      className="text-sm"
                    />
                  </div>

                  <div>
                    <Label className="text-xs mb-1 block">Examples (comma-separated)</Label>
                    <Input
                      placeholder='e.g. Deploy my app to staging, Scale the web service to 3 replicas'
                      value={(skill.examples || []).join(", ")}
                      onChange={(e) => updateSkill(index, { examples: parseCommaSeparated(e.target.value) })}
                      disabled={disabled}
                      className="text-sm"
                    />
                  </div>

                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <Label className="text-xs mb-1 block">Input Modes (comma-separated)</Label>
                      <Input
                        placeholder="e.g. text, file"
                        value={(skill.inputModes || []).join(", ")}
                        onChange={(e) => updateSkill(index, { inputModes: parseCommaSeparated(e.target.value) })}
                        disabled={disabled}
                        className="text-sm"
                      />
                    </div>
                    <div>
                      <Label className="text-xs mb-1 block">Output Modes (comma-separated)</Label>
                      <Input
                        placeholder="e.g. text, file"
                        value={(skill.outputModes || []).join(", ")}
                        onChange={(e) => updateSkill(index, { outputModes: parseCommaSeparated(e.target.value) })}
                        disabled={disabled}
                        className="text-sm"
                      />
                    </div>
                  </div>
                </div>
              )}
            </div>
          );
        })}
      </div>

      <Button
        variant="outline"
        size="sm"
        onClick={addSkill}
        disabled={disabled || skills.length >= 20}
        className="w-full"
      >
        <PlusCircle className="h-4 w-4 mr-2" />
        Add A2A Skill
      </Button>

      {error && (
        <p className="text-red-500 text-sm mt-1">{error}</p>
      )}
    </div>
  );
}
