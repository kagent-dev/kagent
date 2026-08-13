import { Lock } from "lucide-react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

export function SandboxBadge() {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex items-center" aria-label="Sandbox: Agent Substrate">
          <Lock className="h-3.5 w-3.5 text-muted-foreground/70 hover:text-muted-foreground transition-colors" />
        </span>
      </TooltipTrigger>
      <TooltipContent side="top">Agent Substrate</TooltipContent>
    </Tooltip>
  );
}
