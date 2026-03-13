import React from "react";
import { ChevronUp, ChevronDown } from "lucide-react";
import { ScrollArea } from "@radix-ui/react-scroll-area";

interface CollapsibleSectionProps {
  icon: React.ComponentType<{ className?: string }>;
  expanded: boolean;
  onToggle: () => void;
  previewContent: React.ReactNode;
  expandedContent: React.ReactNode;
  errorStyle?: boolean;
}

export function CollapsibleSection({
  icon: Icon,
  expanded,
  onToggle,
  previewContent,
  expandedContent,
  errorStyle,
}: CollapsibleSectionProps) {
  if (!expanded) {
    return (
      <button
        type="button"
        onClick={onToggle}
        className="block w-full text-left cursor-pointer rounded-md hover:bg-muted/40 transition-colors"
      >
        <div className="flex items-start gap-1.5">
          <Icon className="w-3.5 h-3.5 shrink-0 mt-0.5 text-muted-foreground" />
          <div className="flex-1 min-w-0">
            <div className="relative max-h-20 overflow-hidden">
              {previewContent}
            </div>
          </div>
        </div>
        <div className="flex justify-center pt-0.5 text-muted-foreground">
          <ChevronDown className="w-3.5 h-3.5" />
        </div>
      </button>
    );
  }

  return (
    <div className="rounded-md">
      <div className="flex items-start gap-1.5">
        <Icon className="w-3.5 h-3.5 shrink-0 mt-0.5 text-muted-foreground" />
        <div className="flex-1 min-w-0">
          <div className={`relative rounded-md ${errorStyle ? "bg-red-50 dark:bg-red-950/10" : ""}`}>
            <ScrollArea className="max-h-96 overflow-y-auto p-2 w-full rounded-md bg-muted/50">
              {expandedContent}
            </ScrollArea>
          </div>
        </div>
      </div>
      <button
        type="button"
        onClick={onToggle}
        className="flex justify-center w-full pt-0.5 text-muted-foreground cursor-pointer hover:text-foreground transition-colors"
      >
        <ChevronUp className="w-3.5 h-3.5" />
      </button>
    </div>
  );
}
