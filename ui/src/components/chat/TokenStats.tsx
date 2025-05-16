import { ArrowLeft, ArrowRightFromLine } from "lucide-react";
import type { TextMessageConfig } from "@/types/datamodel";
import { TokenStats } from "@/lib/types";

export function calculateTokenStats(prevStats: TokenStats, messageConfig: TextMessageConfig): TokenStats {
  const usage = messageConfig.models_usage;
  if (usage) {
    return {
      total: prevStats.total + usage.prompt_tokens + usage.completion_tokens,
      input: prevStats.input + usage.prompt_tokens,
      output: prevStats.output + usage.completion_tokens,
    };
  }


  return prevStats;
}

interface TokenStatsDisplayProps {
  stats: TokenStats;
}

export default function TokenStatsDisplay({ stats }: TokenStatsDisplayProps) {
  return (
    <div className="flex items-center gap-2 text-xs">
      <span>Usage: </span>
      <span>{stats.total}</span>
      <div className="flex items-center gap-2">
        <div className="flex items-center gap-1">
          <ArrowLeft className="h-3 w-3" />
          <span>{stats.input}</span>
        </div>
        <div className="flex items-center gap-1">
          <ArrowRightFromLine className="h-3 w-3" />
          <span>{stats.output}</span>
        </div>
      </div>
    </div>
  );
}
