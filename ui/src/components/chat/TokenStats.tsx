import { ArrowLeft, ArrowRightFromLine } from "lucide-react";
import { TokenStats } from "@/types";

interface SessionTokenStatsDisplayProps {
  stats: TokenStats;
}

export default function SessionTokenStatsDisplay({ stats }: SessionTokenStatsDisplayProps) {
  return (
    <div className="flex items-center gap-2 text-xs">
      <span>Usage: </span>
      <span>{stats.total}</span>
      <div className="flex items-center gap-2">
        <div className="flex items-center gap-1">
          <ArrowLeft className="h-3 w-3" />
          <span>{stats.prompt}</span>
        </div>
        <div className="flex items-center gap-1">
          <ArrowRightFromLine className="h-3 w-3" />
          <span>{stats.completion}</span>
        </div>
      </div>
    </div>
  );
}
