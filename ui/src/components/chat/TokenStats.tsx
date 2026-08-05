"use client";

import { ArrowLeft, ArrowRightFromLine } from "lucide-react";
import { TokenStats } from "@/types";
import { formatTokens, getModelContextWindow } from "@/lib/tokenUtils";
import { useChatModelInfo } from "@/components/chat/ChatAgentContext";

interface SessionTokenStatsDisplayProps {
  stats: TokenStats;
  /** Prompt tokens of the latest turn — approximates the current context size. */
  contextTokens?: number;
}

export default function SessionTokenStatsDisplay({ stats, contextTokens }: SessionTokenStatsDisplayProps) {
  const modelInfo = useChatModelInfo();
  const contextWindow = getModelContextWindow(modelInfo?.model);

  const usagePercent =
    contextWindow && contextTokens ? Math.min(100, Math.round((contextTokens / contextWindow) * 100)) : undefined;
  const barColor =
    usagePercent === undefined
      ? ""
      : usagePercent > 95
        ? "bg-red-500"
        : usagePercent > 80
          ? "bg-amber-500"
          : "bg-emerald-500";

  return (
    <div className="flex items-center gap-2 text-xs" title={`Total ${stats.total} · prompt ${stats.prompt} · completion ${stats.completion}`}>
      <span>Usage: </span>
      <span>{formatTokens(stats.total)}</span>
      <div className="flex items-center gap-2">
        <div className="flex items-center gap-1">
          <ArrowLeft className="h-3 w-3" />
          <span>{formatTokens(stats.prompt)}</span>
        </div>
        <div className="flex items-center gap-1">
          <ArrowRightFromLine className="h-3 w-3" />
          <span>{formatTokens(stats.completion)}</span>
        </div>
      </div>
      {contextWindow && contextTokens !== undefined && contextTokens > 0 && (
        <div
          className="flex items-center gap-1.5"
          title={`Approximate context: ${contextTokens} of ${contextWindow} tokens (${usagePercent}%)`}
        >
          <span className="text-muted-foreground">·</span>
          <span>
            ctx {formatTokens(contextTokens)}/{formatTokens(contextWindow)}
          </span>
          <span className="inline-block h-1.5 w-12 overflow-hidden rounded-full bg-muted">
            <span className={`block h-full rounded-full ${barColor}`} style={{ width: `${usagePercent}%` }} />
          </span>
        </div>
      )}
    </div>
  );
}
