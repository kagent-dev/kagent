import { useMemo, useState } from "react";
import { FunctionCall } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { convertToUserFriendlyName, isAgentToolName } from "@/lib/utils";
import { ChevronDown, ChevronUp, MessageSquare, Loader2, AlertCircle, CheckCircle } from "lucide-react";
import KagentLogo from "../kagent-logo";
import ToolDisplay, { ToolCallStatus } from "@/components/ToolDisplay";

export type AgentCallStatus = "requested" | "executing" | "completed";

// Constants
const MAX_NESTING_DEPTH = 10;
const NESTING_INDENT_REM = 1.5;

interface NestedToolCall {
  id: string;
  call: FunctionCall;
  result?: {
    content: string;
    is_error?: boolean;
  };
  status: ToolCallStatus;
  nestedCalls?: NestedToolCall[];
}

interface AgentCallDisplayProps {
  call: FunctionCall;
  result?: {
    content: string;
    is_error?: boolean;
  };
  status?: AgentCallStatus;
  isError?: boolean;
  nestedCalls?: NestedToolCall[]; // Support for nested agent/tool calls
  depth?: number; // Track nesting depth for visual indentation
}

const AgentCallDisplay = ({ call, result, status = "requested", isError = false, nestedCalls = [], depth = 0 }: AgentCallDisplayProps) => {
  const [areInputsExpanded, setAreInputsExpanded] = useState(false);
  const [areResultsExpanded, setAreResultsExpanded] = useState(false);
  const [areNestedCallsExpanded, setAreNestedCallsExpanded] = useState(true); // Expanded by default for better visibility

  const agentDisplay = useMemo(() => convertToUserFriendlyName(call.name), [call.name]);
  const hasResult = result !== undefined;
  const hasNestedCalls = nestedCalls && nestedCalls.length > 0;

  // Protection against infinite recursion
  if (depth > MAX_NESTING_DEPTH) {
    console.warn(`Maximum nesting depth (${MAX_NESTING_DEPTH}) reached for agent call:`, call.name);
    return (
      <div className="p-2 text-xs text-muted-foreground border border-yellow-500 rounded">
        ⚠️ Maximum nesting depth reached
      </div>
    );
  }

  // Calculate left margin based on nesting depth
  const marginLeft = depth > 0 ? `${depth * NESTING_INDENT_REM}rem` : '0';

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
    <div style={{ marginLeft }}>
      <Card className={`w-full mx-auto my-1 min-w-full ${isError ? 'border-red-300' : ''} ${depth > 0 ? 'border-l-4 border-l-blue-400' : ''}`}>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-xs flex space-x-5">
            <div className="flex items-center font-medium">
              <KagentLogo className="w-4 h-4 mr-2" />
              {agentDisplay}
              {depth > 0 && <span className="ml-2 text-xs text-muted-foreground">(nested level {depth})</span>}
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

          {/* Nested agent/tool calls section */}
          {hasNestedCalls && (
            <div className="mt-4 border-t pt-4">
              <button
                className="text-xs flex items-center gap-2 font-semibold mb-2"
                onClick={() => setAreNestedCallsExpanded(!areNestedCallsExpanded)}
              >
                <span>Delegated Calls ({nestedCalls.length})</span>
                {areNestedCallsExpanded ? <ChevronUp className="w-4 h-4 ml-1" /> : <ChevronDown className="w-4 h-4 ml-1" />}
              </button>
              {areNestedCallsExpanded && (
                <div className="space-y-2 mt-2">
                  {nestedCalls.map((nestedCall) => (
                    isAgentToolName(nestedCall.call.name) ? (
                      <AgentCallDisplay
                        key={nestedCall.id}
                        call={nestedCall.call}
                        result={nestedCall.result}
                        status={nestedCall.status}
                        isError={nestedCall.result?.is_error}
                        nestedCalls={nestedCall.nestedCalls}
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
    </div>
  );
};

export default AgentCallDisplay;


