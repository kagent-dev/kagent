import { createContext, useContext, useMemo, useState, useEffect } from "react";
import { FunctionCall, TokenStats } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { convertToUserFriendlyName, isAgentToolName } from "@/lib/utils";
import { ChevronDown, ChevronUp, MessageSquare, Loader2, AlertCircle, CheckCircle, Activity } from "lucide-react";
import KagentLogo from "../kagent-logo";
import TokenStatsTooltip from "@/components/chat/TokenStatsTooltip";
import { getSubagentSessionWithEvents } from "@/app/actions/sessions";
import { Message, Task } from "@a2a-js/sdk";
import { extractMessagesFromTasks } from "@/lib/messageHandlers";
import ChatMessage from "@/components/chat/ChatMessage";
import { SmartContent, parseJsonOrString } from "./SmartContent";
import { CollapsibleSection } from "./CollapsibleSection";

// Track and avoid too deep nested agent viewing to avoid UI issues
// In theory this works for infinite depth
const MAX_ACTIVITY_DEPTH = 3;
const ActivityDepthContext = createContext(0);

export type AgentCallStatus = "requested" | "executing" | "completed";

interface AgentCallDisplayProps {
  call: FunctionCall;
  result?: {
    content: string;
    is_error?: boolean;
  };
  status?: AgentCallStatus;
  isError?: boolean;
  tokenStats?: TokenStats;
  subagentSessionId?: string;
}

interface SubagentActivityPanelProps {
  sessionId: string;
  isComplete: boolean;
}

function SubagentActivityPanel({ sessionId, isComplete }: SubagentActivityPanelProps) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [waiting, setWaiting] = useState(true);

  useEffect(() => {
    let cancelled = false;
    let timeoutId: ReturnType<typeof setTimeout> | undefined;

    const fetchEvents = async () => {
      try {
        const resp = await getSubagentSessionWithEvents(sessionId);
        if (cancelled) return;
        if (resp.error || !resp.data) {
          // Treat 404 / empty responses as "not ready yet" — the subagent
          // session may not exist in the DB until it processes the request.
          if (!isComplete) {
            setWaiting(true);
          } else {
            setError(resp.error || "Failed to load subagent activity.");
          }
        } else {
          const tasks: Task[] = resp.data.tasks;
          const extracted = extractMessagesFromTasks(tasks);
          setMessages(extracted);
          setWaiting(extracted.length === 0 && !isComplete);
          setError(null);
        }
      } catch {
        // Network errors during polling are expected (e.g. session not
        // created yet). Only surface as a real error once the subagent
        // has completed and we still can't fetch.
        if (!cancelled && isComplete) {
          setError("Failed to load subagent activity.");
        }
      }

      // Keep polling while subagent is still running
      if (!cancelled && !isComplete) {
        timeoutId = setTimeout(fetchEvents, 2000);
      }
    };

    fetchEvents();
    return () => {
       cancelled = true;
       if (timeoutId) {
         clearTimeout(timeoutId);
       }
     };
  }, [sessionId, isComplete]);

  if (error) {
    return (
      <div className="flex items-center gap-2 text-xs text-red-500 py-2">
        <AlertCircle className="w-3 h-3 shrink-0" />
        {error}
      </div>
    );
  }

  if (waiting && messages.length === 0) {
    return (
      <div className="flex items-center gap-2 py-3 text-muted-foreground text-xs">
        <Loader2 className="w-3 h-3 animate-spin" />
        Checking subagent activity...
      </div>
    );
  }

  if (messages.length === 0) {
    return (
      <p className="text-xs text-muted-foreground py-2">No activity recorded for this session.</p>
    );
  }

  return (
    <div className="space-y-1 mt-1">
      {messages.map((msg) => (
        <ChatMessage
          key={msg.messageId}
          message={msg}
          allMessages={messages}
          // Read-only: no approve/reject/ask-user callbacks
        />
      ))}
    </div>
  );
}

const AgentCallDisplay = ({ call, result, status = "requested", isError = false, tokenStats, subagentSessionId }: AgentCallDisplayProps) => {
  const [areInputsExpanded, setAreInputsExpanded] = useState(false);
  const [areResultsExpanded, setAreResultsExpanded] = useState(false);
  const [activityExpanded, setActivityExpanded] = useState(false);

  const activityDepth = useContext(ActivityDepthContext);
  const agentDisplay = useMemo(() => convertToUserFriendlyName(call.name), [call.name]);
  const hasResult = result !== undefined;
const showActivitySection = !!subagentSessionId && !isError && activityDepth < MAX_ACTIVITY_DEPTH;

  const isAgent = isAgentToolName(call.name);

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

  const parsedResult = hasResult && result?.content ? parseJsonOrString(result.content) : null;
  const argsContent = <SmartContent data={call.args} />;
  const resultContent = parsedResult !== null
    ? <SmartContent data={parsedResult} className={isError ? "text-red-600 dark:text-red-400" : ""} />
    : null;

  return (
    <Card className={`w-full mx-auto my-1 min-w-full ${isError ? 'border-red-300' : ''}`}>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-xs flex space-x-5">
          <div className="flex items-center font-medium">
            <KagentLogo className="w-4 h-4 mr-2" />
            {agentDisplay}
          </div>
          <div className="font-light">
            {call.id}
          </div>
        </CardTitle>
        <div className="flex items-center gap-2 text-xs">
          {tokenStats && <TokenStatsTooltip stats={tokenStats} />}
          {getStatusDisplay()}
        </div>
      </CardHeader>
      <CardContent className="space-y-1 pt-0">
        <CollapsibleSection
          icon={MessageSquare}
          expanded={areInputsExpanded}
          onToggle={() => setAreInputsExpanded(!areInputsExpanded)}
          previewContent={argsContent}
          expandedContent={argsContent}
        />
        {status === "executing" && !hasResult && (
          <div className="flex items-center gap-2 py-1">
            <Loader2 className="h-4 w-4 animate-spin" />
            <span className="text-sm">{agentDisplay} is responding...</span>
          </div>
        )}
        {hasResult && resultContent && (
          <CollapsibleSection
            icon={MessageSquare}
            expanded={areResultsExpanded}
            onToggle={() => setAreResultsExpanded(!areResultsExpanded)}
            previewContent={resultContent}
            expandedContent={resultContent}
            errorStyle={isError}
          />
        )}

        {showActivitySection && (
          <div className="mt-4 border-t pt-3">
            <button
              className="text-xs flex items-center gap-2 text-muted-foreground hover:text-foreground transition-colors"
              onClick={() => setActivityExpanded(!activityExpanded)}
            >
              <Activity className="w-4 h-4" />
              <span>Activity</span>
              {activityExpanded ? <ChevronUp className="w-4 h-4 ml-1" /> : <ChevronDown className="w-4 h-4 ml-1" />}
            </button>
            {activityExpanded && (
              <ActivityDepthContext.Provider value={activityDepth + 1}>
                <div className="mt-2 border rounded bg-muted/20 p-2 max-h-96 overflow-y-auto">
                  <SubagentActivityPanel sessionId={subagentSessionId} isComplete={status === "completed"} />
                </div>
              </ActivityDepthContext.Provider>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
};

export default AgentCallDisplay;
