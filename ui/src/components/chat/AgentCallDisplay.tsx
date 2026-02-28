import { useMemo, useState } from "react";
import { FunctionCall } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { convertToUserFriendlyName, isAgentToolName } from "@/lib/utils";
import { ChevronDown, ChevronUp, MessageSquare, Loader2, AlertCircle, CheckCircle, GitBranch } from "lucide-react";
import KagentLogo from "../kagent-logo";
import ToolDisplay from "@/components/ToolDisplay";
import type { ToolCallState } from "@/components/chat/ToolCallDisplay";

export type AgentCallStatus = "requested" | "executing" | "completed";

const MAX_NESTING_DEPTH = 10;

interface AgentCallDisplayProps {
  call: FunctionCall;
  result?: {
    content: string;
    is_error?: boolean;
  };
  status?: AgentCallStatus;
  isError?: boolean;
  nestedCalls?: ToolCallState[];
  depth?: number;
}

const AgentCallDisplay = ({ call, result, status = "requested", isError = false, nestedCalls, depth = 0 }: AgentCallDisplayProps) => {
  const [areInputsExpanded, setAreInputsExpanded] = useState(false);
  const [areResultsExpanded, setAreResultsExpanded] = useState(false);
  const [areDelegatedExpanded, setAreDelegatedExpanded] = useState(false);

  const agentDisplay = useMemo(() => convertToUserFriendlyName(call.name), [call.name]);
  const hasResult = result !== undefined;
  const hasNestedCalls = nestedCalls && nestedCalls.length > 0;

  // Build nested-of-nested: for each nested agent call, find its children among the
  // remaining nested calls using ordering-based disambiguation.
  const nestedCallsMap = useMemo(() => {
    if (!nestedCalls || depth >= MAX_NESTING_DEPTH) return new Map<string, ToolCallState[]>();

    const NAMESPACE_SEPARATOR = "__NS__";
    const map = new Map<string, ToolCallState[]>();

    // Single pass: assign each child to the most recent preceding agent call
    // for the matching sub-agent name (disambiguates repeat invocations).
    const activeParent = new Map<string, string>();

    for (const nc of nestedCalls) {
      if (isAgentToolName(nc.call.name)) {
        const lastIdx = nc.call.name.lastIndexOf(NAMESPACE_SEPARATOR);
        if (lastIdx === -1) continue;
        const subAgentName = nc.call.name.substring(lastIdx + NAMESPACE_SEPARATOR.length);
        activeParent.set(subAgentName, nc.id);
        if (!map.has(nc.id)) {
          map.set(nc.id, []);
        }
      } else if (nc.author) {
        const parentId = activeParent.get(nc.author);
        if (parentId) {
          map.get(parentId)!.push(nc);
        }
      }
    }

    // Remove empty entries
    for (const [id, children] of map) {
      if (children.length === 0) map.delete(id);
    }

    return map;
  }, [nestedCalls, depth]);

  // IDs that are children of nested agent calls (should not render at top level of this section)
  const subNestedIds = useMemo(() => {
    const ids = new Set<string>();
    nestedCallsMap.forEach(children => {
      for (const child of children) {
        ids.add(child.id);
      }
    });
    return ids;
  }, [nestedCallsMap]);

  const topLevelNestedCalls = useMemo(() => {
    if (!nestedCalls) return [];
    return nestedCalls.filter(nc => !subNestedIds.has(nc.id));
  }, [nestedCalls, subNestedIds]);

  const getStatusDisplay = () => {
    if (isError && status === "executing") {
      return (
        <>
          <AlertCircle className="w-3 h-3 inline-block mr-2 text-red-500" />
          Error
        </>
      );
    }
    switch (status) {
      case "requested":
        return (
          <>
            <KagentLogo className="w-3 h-3 inline-block mr-2 text-blue-500" />
            Delegating
          </>
        );
      case "executing":
        return (
          <>
            <Loader2 className="w-3 h-3 inline-block mr-2 text-yellow-500 animate-spin" />
            Awaiting response
          </>
        );
      case "completed":
        if (isError) {
          return (
            <>
              <AlertCircle className="w-3 h-3 inline-block mr-2 text-red-500" />
              Failed
            </>
          );
        }
        return (
          <>
            <CheckCircle className="w-3 h-3 inline-block mr-2 text-green-500" />
            Completed
          </>
        );
      default:
        return null;
    }
  };

  return (
    <Card className={`w-full mx-auto my-1 min-w-full ${isError ? 'border-red-300' : ''}`}>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-xs flex space-x-5">
          <div className="flex items-center font-medium">
            <KagentLogo className="w-4 h-4 mr-2" />
            {agentDisplay}
          </div>
          <div className="font-light">{call.id}</div>
        </CardTitle>
        <div className="flex justify-center items-center text-xs">
          {getStatusDisplay()}
        </div>
      </CardHeader>
      <CardContent>
        <div className="space-y-2 mt-2">
          <button className="text-xs flex items-center gap-2" onClick={() => setAreInputsExpanded(!areInputsExpanded)}>
            <MessageSquare className="w-4 h-4" />
            <span>Input</span>
            {areInputsExpanded ? <ChevronUp className="w-4 h-4 ml-1" /> : <ChevronDown className="w-4 h-4 ml-1" />}
          </button>
          {areInputsExpanded && (
            <div className="mt-2 bg-muted/50 p-3 rounded">
              <pre className="text-sm whitespace-pre-wrap break-words">{JSON.stringify(call.args, null, 2)}</pre>
            </div>
          )}
        </div>

        <div className="mt-4 w-full">
          {status === "executing" && !hasResult && (
            <div className="flex items-center gap-2 py-2">
              <Loader2 className="h-4 w-4 animate-spin" />
              <span className="text-sm">{agentDisplay} is responding...</span>
            </div>
          )}
          {hasResult && result?.content && (
            <div className="space-y-2">
              <button className="text-xs flex items-center gap-2" onClick={() => setAreResultsExpanded(!areResultsExpanded)}>
                <MessageSquare className="w-4 h-4" />
                <span>Output</span>
                {areResultsExpanded ? <ChevronUp className="w-4 h-4 ml-1" /> : <ChevronDown className="w-4 h-4 ml-1" />}
              </button>
              {areResultsExpanded && (
                <div className={`mt-2 ${isError ? 'bg-red-50 dark:bg-red-950/10' : 'bg-muted/50'} p-3 rounded`}>
                  <pre className={`text-sm whitespace-pre-wrap break-words ${isError ? 'text-red-600 dark:text-red-400' : ''}`}>
                    {result?.content}
                  </pre>
                </div>
              )}
            </div>
          )}
        </div>

        {hasNestedCalls && depth < MAX_NESTING_DEPTH && (
          <div className="mt-4">
            <button
              className="text-xs flex items-center gap-2"
              onClick={() => setAreDelegatedExpanded(!areDelegatedExpanded)}
            >
              <GitBranch className="w-4 h-4" />
              <span>Delegated Calls ({topLevelNestedCalls.length})</span>
              {areDelegatedExpanded ? <ChevronUp className="w-4 h-4 ml-1" /> : <ChevronDown className="w-4 h-4 ml-1" />}
            </button>
            {areDelegatedExpanded && (
              <div className="mt-2 pl-3 border-l-2 border-muted-foreground/20 space-y-2">
                {topLevelNestedCalls.map(nestedCall => (
                  isAgentToolName(nestedCall.call.name) ? (
                    <AgentCallDisplay
                      key={nestedCall.id}
                      call={nestedCall.call}
                      result={nestedCall.result}
                      status={nestedCall.status}
                      isError={nestedCall.result?.is_error}
                      nestedCalls={nestedCallsMap.get(nestedCall.id)}
                      depth={depth + 1}
                    />
                  ) : (
                    <ToolDisplay
                      key={nestedCall.id}
                      call={nestedCall.call}
                      result={nestedCall.result}
                      status={nestedCall.status}
                      isError={nestedCall.result?.is_error}
                    />
                  )
                ))}
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
};

export default AgentCallDisplay;
