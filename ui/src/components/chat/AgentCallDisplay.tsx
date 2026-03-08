import { useCallback, useMemo, useState } from "react";
import { FunctionCall } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { convertToUserFriendlyName } from "@/lib/utils";
import { ChevronDown, ChevronUp, MessageSquare, Loader2, AlertCircle, CheckCircle, ExternalLink } from "lucide-react";
import KagentLogo from "../kagent-logo";
import { getSubAgentSession } from "@/app/actions/sessions";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";

export type AgentCallStatus = "requested" | "executing" | "completed";

interface AgentCallDisplayProps {
  call: FunctionCall;
  result?: {
    content: string;
    is_error?: boolean;
  };
  status?: AgentCallStatus;
  isError?: boolean;
  sessionId?: string;
}

const AgentCallDisplay = ({ call, result, status = "requested", isError = false, sessionId }: AgentCallDisplayProps) => {
  const [areInputsExpanded, setAreInputsExpanded] = useState(false);
  const [areResultsExpanded, setAreResultsExpanded] = useState(false);

  const agentDisplay = useMemo(() => convertToUserFriendlyName(call.name), [call.name]);
  const hasResult = result !== undefined;

  const callId = call.id;

  const onOpenSubAgentSession = useCallback(async () => {
    try {
      // Theoretically we must have a session ID to be rendered, check anyway
      if(!sessionId) {
        toast.error('No session ID, cannot lookup sub-agent session');
        return;
      }

      const response = await getSubAgentSession(sessionId, callId);
      if (response.data && response.data.id) {
        const subagentSessionUrl = `/agents/${agentDisplay}/chat/${response.data.id}`;
        window.open(subagentSessionUrl, '_blank');
      } else {
        toast.error('Sub-agent session not found');
      }
    } catch (error) {
      console.error('Error opening subagent session:', error);
      toast.error('Failed to open sub-agent session');
    }
  }, [agentDisplay, sessionId, callId]);

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
          <Button
              className="h-5 w-5 font-light"
              size="icon"
              variant="ghost"
              onClick={onOpenSubAgentSession}>
            <ExternalLink className="w-3 h-3" />
          </Button>
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
      </CardContent>
    </Card>
  );
};

export default AgentCallDisplay;


