import React, { useState } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Settings2, Plus, Trash2, Shield } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import axios from 'axios';

export interface AgentSkill {
  id: string;
  name: string;
  description?: string;
  examples?: string[];
  inputModes?: string[];
  outputModes?: string[];
  tags?: string[];
}

export interface A2AAuthState {
  enabled: boolean;
  auth: {
    type: 'jwt' | 'apiKey' | 'none';
    audience?: string;
    issuer?: string;
    expiresIn?: number;
  };
  skills: AgentSkill[];
}

interface A2AAuthSectionProps {
  value: A2AAuthState;
  onChange: (value: A2AAuthState) => void;
  error?: string;
  disabled?: boolean;
}

export function A2AAuthSection({ value, onChange, error, disabled }: A2AAuthSectionProps) {
  const [showToken, setShowToken] = useState(false);
  const [token, setToken] = useState('');
  const [generating, setGenerating] = useState(false);

  const handleSkillChange = (index: number, field: keyof AgentSkill, newValue: string | string[]) => {
    const updatedSkills = [...value.skills];
    updatedSkills[index] = {
      ...updatedSkills[index],
      [field]: newValue
    };
    onChange({ ...value, skills: updatedSkills });
  };

  const addSkill = () => {
    onChange({
      ...value,
      skills: [
        ...value.skills,
        {
          id: '',
          name: '',
          description: '',
          examples: [],
          inputModes: [],
          outputModes: [],
          tags: []
        }
      ]
    });
  };

  const removeSkill = (index: number) => {
    const updatedSkills = value.skills.filter((_, i) => i !== index);
    onChange({ ...value, skills: updatedSkills });
  };

  const handleGenerateToken = async () => {
    setGenerating(true);
    try {
      const res = await axios.post('/api/generate-a2a-token', {
        audience: value.auth.audience,
        issuer: value.auth.issuer,
        type: value.auth.type || 'HS256',
        expiresIn: value.auth.expiresIn ? value.auth.expiresIn * 3600 : 24 * 3600,
      });
      setToken(res.data.token);
      setShowToken(true);
    } catch (error) {
      console.error('Failed to generate token:', error);
      alert('Failed to generate token');
    } finally {
      setGenerating(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Settings2 className="h-5 w-5 text-blue-500" />
          A2A Configuration
        </CardTitle>
        <p className="text-xs mb-2 block text-muted-foreground">
          Configure agent-to-agent communication and skills.
        </p>
      </CardHeader>
      <CardContent className="space-y-6">
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <label className="text-sm font-medium">Enable A2A</label>
            <Switch
              checked={value.enabled}
              onCheckedChange={(checked) => onChange({ 
                ...value, 
                enabled: checked,
                auth: checked ? value.auth : { type: 'none' }
              })}
              disabled={disabled}
            />
          </div>

          {value.enabled && (
            <>
              <div className="space-y-4">
                <div className="flex items-center justify-between">
                  <h3 className="text-lg font-medium">Authentication</h3>
                </div>

                <div className="space-y-4">
                  <div>
                    <label className="text-sm font-medium">Type</label>
                    <Select
                      value={value.auth.type}
                      onValueChange={(type) => onChange({
                        ...value,
                        auth: { ...value.auth, type: type as 'jwt' | 'apiKey' | 'none' }
                      })}
                      disabled={disabled}
                    >
                      <SelectTrigger>
                        <SelectValue placeholder="Select auth type" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="none">None</SelectItem>
                        <SelectItem value="jwt">JWT</SelectItem>
                        <SelectItem value="apiKey">API Key</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  {value.auth.type === 'jwt' && (
                    <>
                      <div>
                        <label className="text-sm font-medium">Audience</label>
                        <Input
                          value={value.auth.audience || ''}
                          onChange={(e) => onChange({
                            ...value,
                            auth: { ...value.auth, audience: e.target.value }
                          })}
                          placeholder="Enter audience"
                          disabled={disabled}
                        />
                      </div>

                      <div>
                        <label className="text-sm font-medium">Issuer</label>
                        <Input
                          value={value.auth.issuer || ''}
                          onChange={(e) => onChange({
                            ...value,
                            auth: { ...value.auth, issuer: e.target.value }
                          })}
                          placeholder="Enter issuer"
                          disabled={disabled}
                        />
                      </div>

                      <div>
                        <label className="text-sm font-medium">Expiration (hours)</label>
                        <Input
                          type="number"
                          value={value.auth.expiresIn || 24}
                          onChange={(e) => onChange({
                            ...value,
                            auth: { ...value.auth, expiresIn: parseInt(e.target.value) }
                          })}
                          placeholder="Enter expiration in hours"
                          disabled={disabled}
                        />
                      </div>

                      <div className="flex items-center gap-2">
                        <Button
                          type="button"
                          variant="outline"
                          onClick={handleGenerateToken}
                          disabled={disabled || generating || !value.auth.audience || !value.auth.issuer}
                        >
                          {generating ? 'Generating...' : 'Generate Token'}
                        </Button>
                        {showToken && (
                          <div className="flex-1">
                            <Input
                              value={token}
                              readOnly
                              className="font-mono text-sm"
                            />
                          </div>
                        )}
                      </div>
                    </>
                  )}
                </div>
              </div>

              <div className="space-y-4">
                <div className="flex items-center justify-between">
                  <h3 className="text-lg font-medium">Skills</h3>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={addSkill}
                    disabled={disabled}
                  >
                    <Plus className="h-4 w-4 mr-2" />
                    Add Skill
                  </Button>
                </div>

                {value.skills.map((skill, index) => (
                  <Card key={index} className="p-4">
                    <div className="flex justify-between items-start mb-4">
                      <h4 className="text-md font-medium">Skill {index + 1}</h4>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => removeSkill(index)}
                        disabled={disabled}
                      >
                        <Trash2 className="h-4 w-4 text-red-500" />
                      </Button>
                    </div>

                    <div className="space-y-4">
                      <div>
                        <label className="text-sm font-medium">ID</label>
                        <Input
                          value={skill.id}
                          onChange={(e) => handleSkillChange(index, 'id', e.target.value)}
                          placeholder="Enter skill ID"
                          disabled={disabled}
                        />
                      </div>

                      <div>
                        <label className="text-sm font-medium">Name</label>
                        <Input
                          value={skill.name}
                          onChange={(e) => handleSkillChange(index, 'name', e.target.value)}
                          placeholder="Enter skill name"
                          disabled={disabled}
                        />
                      </div>

                      <div>
                        <label className="text-sm font-medium">Description</label>
                        <Textarea
                          value={skill.description || ''}
                          onChange={(e) => handleSkillChange(index, 'description', e.target.value)}
                          placeholder="Enter skill description"
                          disabled={disabled}
                        />
                      </div>

                      <div>
                        <label className="text-sm font-medium">Examples (one per line)</label>
                        <Textarea
                          value={skill.examples?.join('\n') || ''}
                          onChange={(e) => handleSkillChange(index, 'examples', e.target.value.split('\n').filter(Boolean))}
                          placeholder="Enter examples, one per line"
                          disabled={disabled}
                        />
                      </div>

                      <div>
                        <label className="text-sm font-medium">Tags (comma-separated)</label>
                        <Input
                          value={skill.tags?.join(', ') || ''}
                          onChange={(e) => handleSkillChange(index, 'tags', e.target.value.split(',').map(tag => tag.trim()).filter(Boolean))}
                          placeholder="Enter tags, comma-separated"
                          disabled={disabled}
                        />
                      </div>
                    </div>
                  </Card>
                ))}
              </div>
            </>
          )}
        </div>

        {error && <p className="text-red-500 text-sm">{error}</p>}
      </CardContent>
    </Card>
  );
} 