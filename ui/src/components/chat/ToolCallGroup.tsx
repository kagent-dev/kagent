"use client";

import React, { useMemo, useState } from "react";
import { Message } from "@a2a-js/sdk";
import { ChevronRight, Wrench, Loader2, CheckCircle, XCircle } from "lucide-react";
import { cn, convertToUserFriendlyName } from "@/lib/utils";
import { extractToolCallRequests, extractToolCallResults } from "@/lib/toolCallExtraction";
import { ADKMetadata, getMetadataValue } from "@/lib/messageHandlers";

/**
 * A render item produced by {@link groupToolCallMessages}: either a single
 * regular chat message or a run of consecutive tool-call messages that should
 * be rendered inside one collapsible {@link ToolCallGroup}.
 */
export type ChatRenderItem =
  | { kind: "single"; message: Message; startIndex: number }
  | { kind: "group"; messages: Message[]; startIndex: number };

/** Message types that always render standalone, even when tool-related. */
const NEVER_GROUPED_TYPES = new Set(["AskUserRequest", "ToolApprovalRequest"]);

/** Tool-related originalType values that carry no data parts (streaming format). */
const STREAMING_TOOL_TYPES = new Set([
  "ToolCallRequestEvent",
  "ToolCallExecutionEvent",
  "ToolCallSummaryMessage",
]);

/**
 * True when a message renders as tool-call chrome (request/result/summary) and
 * can be folded into a ToolCallGroup. Approval and ask-user messages are
 * excluded: they demand user attention and must never be hidden.
 */
export const isGroupableToolMessage = (message: Message): boolean => {
  if (message.role === "user") return false;

  const metadata = message.metadata as ADKMetadata;
  const originalType = metadata?.originalType;
  if (originalType && NEVER_GROUPED_TYPES.has(originalType)) return false;
  if (originalType && STREAMING_TOOL_TYPES.has(originalType)) return true;

  return (
    message.parts?.some(part => {
      if (part.kind === "data" && part.metadata) {
        const partType = getMetadataValue<string>(part.metadata as Record<string, unknown>, "type");
        return partType === "function_call" || partType === "function_response";
      }
      return false;
    }) ?? false
  );
};

/**
 * Partition a message list into render items, folding consecutive runs of
 * tool-call messages into groups. Text messages, approvals, and ask-user
 * requests break a run and render standalone.
 */
export const groupToolCallMessages = (messages: Message[]): ChatRenderItem[] => {
  const items: ChatRenderItem[] = [];
  let run: Message[] = [];
  let runStart = 0;

  const flush = () => {
    if (run.length === 0) return;
    items.push({ kind: "group", messages: run, startIndex: runStart });
    run = [];
  };

  messages.forEach((message, index) => {
    if (isGroupableToolMessage(message)) {
      if (run.length === 0) runStart = index;
      run.push(message);
    } else {
      flush();
      items.push({ kind: "single", message, startIndex: index });
    }
  });
  flush();

  return items;
};

interface ToolCallGroupSummary {
  total: number;
  passed: number;
  failed: number;
  running: number;
  /** Deduped, user-friendly tool names in call order. */
  names: string[];
}

/**
 * Build a `call_id -> is_error` lookup for every tool result in the
 * transcript. Compute this ONCE per transcript (memoized in the parent) and
 * share it across all ToolCallGroups — results can arrive in messages outside
 * a group's boundary, and per-group transcript scans would be
 * O(#groups × #messages).
 */
export const buildToolCallResultsIndex = (messages: Message[]): Map<string, boolean> => {
  const index = new Map<string, boolean>();
  for (const message of messages) {
    for (const result of extractToolCallResults(message)) {
      if (result.call_id && !index.has(result.call_id)) {
        index.set(result.call_id, !!result.is_error);
      }
    }
  }
  return index;
};

const summarize = (groupMessages: Message[], resultsByCallId: ReadonlyMap<string, boolean>): ToolCallGroupSummary => {
  // id -> tool name for every call requested inside this group
  const requests = new Map<string, string>();
  for (const message of groupMessages) {
    for (const request of extractToolCallRequests(message)) {
      if (request.id) requests.set(request.id, request.name);
    }
  }

  let passed = 0;
  let failed = 0;
  for (const id of requests.keys()) {
    const isError = resultsByCallId.get(id);
    if (isError === undefined) continue; // still running
    if (isError) failed++;
    else passed++;
  }

  return {
    total: requests.size,
    passed,
    failed,
    running: requests.size - passed - failed,
    names: [...new Set([...requests.values()].map(convertToUserFriendlyName))],
  };
};

interface ToolCallGroupProps {
  /** The consecutive tool-call messages folded into this group. */
  messages: Message[];
  /**
   * Shared `call_id -> is_error` lookup built from the full transcript with
   * {@link buildToolCallResultsIndex} (results may live outside the group).
   */
  resultsByCallId: ReadonlyMap<string, boolean>;
  /** The already-rendered tool call displays for the grouped messages. */
  children: React.ReactNode;
}

const MAX_PREVIEW_NAMES = 3;

/**
 * Collapsible wrapper around a run of tool calls in the chat transcript.
 * Collapsed (the default) it renders a single slim summary row — total calls,
 * pass/fail counts, and a live progress indicator while calls are in flight —
 * so long tool-heavy turns stop drowning out the conversation.
 */
const ToolCallGroup = ({ messages, resultsByCallId, children }: ToolCallGroupProps) => {
  const [open, setOpen] = useState(false);
  const summary = useMemo(() => summarize(messages, resultsByCallId), [messages, resultsByCallId]);

  // Nothing visible inside (e.g. only internal/filtered calls) — no chrome.
  if (summary.total === 0) return <>{children}</>;

  const { total, passed, failed, running } = summary;
  const isRunning = running > 0;
  const previewNames = summary.names.slice(0, MAX_PREVIEW_NAMES).join(", ");
  const hiddenNames = summary.names.length - MAX_PREVIEW_NAMES;

  return (
    <div className="w-full min-w-0 max-w-full">
      <button
        type="button"
        onClick={() => setOpen(prev => !prev)}
        aria-expanded={open}
        className="group flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
      >
        <ChevronRight
          aria-hidden
          className={cn("h-3.5 w-3.5 shrink-0 transition-transform duration-200 ease-out", open && "rotate-90")}
        />
        {isRunning ? (
          <Loader2 aria-hidden className="h-3.5 w-3.5 shrink-0 animate-spin" />
        ) : (
          <Wrench aria-hidden className="h-3.5 w-3.5 shrink-0" />
        )}

        <span className="shrink-0 font-medium tabular-nums">
          {isRunning
            ? `Running tools ${passed + failed}/${total}`
            : `${total} tool call${total === 1 ? "" : "s"}`}
        </span>

        {passed > 0 && (
          <span className="flex shrink-0 items-center gap-1 tabular-nums text-emerald-600 dark:text-emerald-400">
            <CheckCircle aria-hidden className="h-3.5 w-3.5" />
            {passed}
            <span className="sr-only">succeeded</span>
          </span>
        )}
        {failed > 0 && (
          <span className="flex shrink-0 items-center gap-1 tabular-nums text-red-600 dark:text-red-400">
            <XCircle aria-hidden className="h-3.5 w-3.5" />
            {failed}
            <span className="sr-only">failed</span>
          </span>
        )}

        {!open && previewNames && (
          <span className="min-w-0 truncate text-muted-foreground/70">
            {previewNames}
            {hiddenNames > 0 && ` +${hiddenNames}`}
          </span>
        )}
      </button>

      <div
        className={cn(
          "grid transition-[grid-template-rows] duration-300 [transition-timing-function:cubic-bezier(0.22,1,0.36,1)]",
          open ? "grid-rows-[1fr]" : "grid-rows-[0fr]",
        )}
      >
        <div className="min-h-0 overflow-hidden" inert={!open}>
          <div className="ml-[0.9375rem] space-y-2 border-l border-border pb-1 pl-4 pt-1">
            {children}
          </div>
        </div>
      </div>
    </div>
  );
};

export default ToolCallGroup;
