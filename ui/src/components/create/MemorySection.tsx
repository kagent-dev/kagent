import React from "react";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { MemoryConfig, MemoryType } from "@/types";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Brain } from "lucide-react";

interface MemorySectionProps {
  config: MemoryConfig | undefined;
  onChange: (config: MemoryConfig | undefined) => void;
  error?: string;
  disabled: boolean;
}

export const MemorySection = ({ config, onChange, error, disabled }: MemorySectionProps) => {
  const type = config?.type || "None";

  const handleTypeChange = (newType: string) => {
    if (newType === "None") {
      onChange(undefined);
      return;
    }
    
    const newConfig: MemoryConfig = { type: newType as MemoryType };
    if (newType === "InMemory") {
      newConfig.inMemory = {};
    } else if (newType === "VertexAI") {
      newConfig.vertexAi = {};
    } else if (newType === "McpServer") {
      newConfig.mcpServer = { name: "" };
    }
    onChange(newConfig);
  };

  const handleVertexChange = (key: string, value: string) => {
    if (type !== "VertexAI") return;
    onChange({
      ...config,
      type: "VertexAI",
      vertexAi: {
        ...config?.vertexAi,
        [key]: value,
      },
    });
  };

  const handleMcpChange = (key: string, value: string) => {
    if (type !== "McpServer") return;
    onChange({
      ...config,
      type: "McpServer",
      mcpServer: {
        name: config?.mcpServer?.name || "",
        ...config?.mcpServer,
        [key]: value,
      },
    });
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Brain className="h-5 w-5 text-pink-500" />
          Memory Configuration
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div>
          <Label className="mb-2 block">Memory Type</Label>
          <Select
            value={type}
            onValueChange={handleTypeChange}
            disabled={disabled}
          >
            <SelectTrigger>
              <SelectValue placeholder="Select memory type" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="None">None</SelectItem>
              <SelectItem value="InMemory">In-Memory</SelectItem>
              <SelectItem value="VertexAI">Vertex AI RAG</SelectItem>
              <SelectItem value="McpServer">MCP Server</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {type === "VertexAI" && (
          <div className="grid grid-cols-2 gap-4 pl-4 border-l-2 border-muted">
            <div>
              <Label>Project ID (optional)</Label>
              <Input
                value={config?.vertexAi?.projectID || ""}
                onChange={(e) => handleVertexChange("projectID", e.target.value)}
                placeholder="GCP Project ID"
                disabled={disabled}
              />
            </div>
            <div>
              <Label>Location (optional)</Label>
              <Input
                value={config?.vertexAi?.location || ""}
                onChange={(e) => handleVertexChange("location", e.target.value)}
                placeholder="e.g. us-central1"
                disabled={disabled}
              />
            </div>
          </div>
        )}

        {type === "McpServer" && (
          <div className="space-y-4 pl-4 border-l-2 border-muted">
            <div>
              <Label>MCP Server Name</Label>
              <Input
                value={config?.mcpServer?.name || ""}
                onChange={(e) => handleMcpChange("name", e.target.value)}
                placeholder="Name of the MCP server resource"
                disabled={disabled}
                className={error ? "border-red-500" : ""}
              />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <Label>Kind (optional)</Label>
                <Input
                  value={config?.mcpServer?.kind || ""}
                  onChange={(e) => handleMcpChange("kind", e.target.value)}
                  placeholder="Default: MCPServer"
                  disabled={disabled}
                />
              </div>
              <div>
                <Label>API Group (optional)</Label>
                <Input
                  value={config?.mcpServer?.apiGroup || ""}
                  onChange={(e) => handleMcpChange("apiGroup", e.target.value)}
                  placeholder="Default: kagent.dev"
                  disabled={disabled}
                />
              </div>
            </div>
          </div>
        )}
        {error && <p className="text-red-500 text-sm mt-2">{error}</p>}
      </CardContent>
    </Card>
  );
};
