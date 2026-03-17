import { BarChart2 } from "lucide-react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { TokenStats } from "@/types";

interface TokenStatsTooltipProps {
  stats: TokenStats;
}

export default function TokenStatsTooltip({ stats }: TokenStatsTooltipProps) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button type="button" className="p-1 rounded-full hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors" aria-label="Token usage">
          <BarChart2 className="w-4 h-4" />
        </button>
      </TooltipTrigger>
      <TooltipContent side="top">
        <div className="flex flex-col gap-1 text-xs">
          <span>Total: {stats.total}</span>
          <span>Prompt: {stats.prompt}</span>
          <span>Completion: {stats.completion}</span>
        </div>
      </TooltipContent>
    </Tooltip>
  );
}
