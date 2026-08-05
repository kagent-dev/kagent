import { BarChart2 } from "lucide-react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { TokenStats } from "@/types";
import { formatTokens } from "@/lib/tokenUtils";

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
          <span>Total: {formatTokens(stats.total)} ({stats.total})</span>
          <span>Prompt: {formatTokens(stats.prompt)} ({stats.prompt})</span>
          <span>Completion: {formatTokens(stats.completion)} ({stats.completion})</span>
        </div>
      </TooltipContent>
    </Tooltip>
  );
}
