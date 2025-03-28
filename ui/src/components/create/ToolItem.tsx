import React from "react";
import { CheckCircle } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Component, ToolConfig } from "@/types/datamodel";

interface ToolItemProps {
  tool: Component<ToolConfig>;
  isSelected: boolean;
  onToggle: (tool: Component<ToolConfig>) => void;
  disabled?: boolean;
  displayName?: string;
  description?: string;
}

const ToolItem: React.FC<ToolItemProps> = ({ tool, isSelected, onToggle, disabled = false, displayName, description }) => {
  const toolName = displayName || tool.label;
  const toolDescription = description || tool.description;
  
  return (
    <div
      className={`p-3 hover:bg-secondary/20 transition-colors cursor-pointer ${disabled && !isSelected ? "opacity-50 cursor-not-allowed" : ""}`}
      onClick={() => !disabled && onToggle(tool)}
    >
      <div className="flex items-start justify-between">
        <div className="flex-1">
          <div className="flex items-center">
            <h4 className="font-medium">{toolName}</h4>
            {tool.provider.startsWith("autogen_ext.tools.mcp") && (
              <Badge variant="outline" className="ml-2 bg-purple-50 text-purple-700 border-purple-200">
                MCP
              </Badge>
            )}
          </div>
          <p className="text-sm text-muted-foreground mt-1">{toolDescription}</p>
          <div className="text-xs text-muted-foreground mt-2">{tool.provider}</div>
        </div>
        <div className="ml-4 flex-shrink-0">
          {isSelected ? (
            <CheckCircle className="h-5 w-5 text-primary" />
          ) : (
            <div className="h-5 w-5 rounded-full border-2 border-muted"></div>
          )}
        </div>
      </div>
    </div>
  );
};

export default ToolItem;