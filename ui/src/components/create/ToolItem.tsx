import {  isMcpTool } from "@/lib/data";
import { Check } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {  Component, ToolConfig } from "@/types/datamodel";


interface ToolItemProps {
  tool: Component<ToolConfig>;
  isSelected: boolean;
  onToggle: (tool: Component<ToolConfig>) => void;
  disabled?: boolean;
}

const ToolItem = ({ tool, isSelected, onToggle, disabled = false }: ToolItemProps) => {
  const displayName = tool.provider; // getToolDisplayName(tool) || "Unnamed Tool";
  const displayDescription = tool.description; // getToolDescription(tool) || "No description available";
  const toolId = tool.provider; // getToolIdentifier(tool);

  // Determine classes based on selection and disabled states
  const containerClasses = `p-4 rounded-lg border transition-all ${
    isSelected 
      ? "bg-primary/10 border-primary/30" 
      : disabled
        ? "border-border bg-gray-50 opacity-60"
        : "border-border hover:bg-secondary/50"
  } ${disabled && !isSelected ? "cursor-not-allowed" : "cursor-pointer"}`;

  const handleClick = () => {
    if (!disabled || isSelected) {
      onToggle(tool);
    }
  };

  return (
    <div
      className={containerClasses}
      onClick={handleClick}
    >
      <div className="flex items-start justify-between gap-4">
        <div className="flex-1">
          <div className="font-medium flex items-center flex-wrap gap-2">
            <span className="mr-1">{displayName}</span>
            <div className="flex flex-wrap gap-1">
              {isMcpTool(tool) ? (
                <Badge variant="outline" className="bg-blue-500/10 text-blue-500 border-blue-500/20 hover:bg-blue-500/20">
                  MCP
                </Badge>
              ) : (
                <Badge variant="outline" className="bg-gray-100 text-gray-500 border-gray-200">
                  {tool.provider}
                </Badge>
              )}
            </div>
          </div>
          <div className="text-muted-foreground mt-2">{displayDescription}</div>
          <div className="text-xs text-muted-foreground mt-1 opacity-70">{toolId}</div>
        </div>
        <Button
          variant={isSelected ? "secondary" : "outline"}
          size="sm"
          className={`mt-0 ${isSelected ? "bg-primary text-primary-foreground hover:bg-primary/90" : ""}`}
          disabled={disabled && !isSelected}
          onClick={(e) => {
            e.stopPropagation();
            handleClick();
          }}
        >
          {isSelected ? (
            <>
              <Check className="w-4 h-4 mr-1" /> Selected
            </>
          ) : (
            "Select"
          )}
        </Button>
      </div>
    </div>
  );
};

export default ToolItem;