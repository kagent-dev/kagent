import { ArrowLeft, ArrowRightFromLine } from "lucide-react";
import type { Message } from "@/types/datamodel";
import { TokenStats } from "@/lib/types";


export function calculateTokenStats(messages: Message[]): TokenStats {
  return messages.reduce(
    (stats, message) => {
      const usage = message.config?.models_usage;
      if (usage) {
        return {
          total: stats.total + (usage.prompt_tokens + usage.completion_tokens),
          input: stats.input + usage.prompt_tokens,
          output: stats.output + usage.completion_tokens,
        };
      }
      return stats;
    },
    { total: 0, input: 0, output: 0 }
  );
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
