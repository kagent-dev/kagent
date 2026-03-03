import { useMemo, useState } from "react";
import Link from "next/link";
import { FunctionCall } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { convertToUserFriendlyName } from "@/lib/utils";
import { MessageSquare, Loader2, AlertCircle, CheckCircle } from "lucide-react";
import KagentLogo from "../kagent-logo";
import { SmartContent, parseContentString } from "./SmartContent";
import { CollapsibleSection } from "./CollapsibleSection";

export type AgentCallStatus = "requested" | "executing" | "completed";

interface AgentCallDisplayProps {
  call: FunctionCall;
  result?: {
    content: string;
    is_error?: boolean;
  };
  status?: AgentCallStatus;
  isError?: boolean;
}

const AGENT_TOOL_NAME_RE = /^(.+)__NS__(.+)$/;



const AgentCallDisplay = ({ call, result, status = "requested", isError = false }: AgentCallDisplayProps) => {
  const [areInputsExpanded, setAreInputsExpanded] = useState(false);
  const [areResultsExpanded, setAreResultsExpanded] = useState(false);

  const agentDisplay = useMemo(() => convertToUserFriendlyName(call.name), [call.name]);
  const hasResult = result !== undefined;

  const agentMatch = call.name.match(AGENT_TOOL_NAME_RE);
  const functionCallLink = agentMatch
    ? `/agents/${agentMatch[1].replace(/_/g, "-")}/${agentMatch[2].replace(/_/g, "-")}/function-calls/${call.id}`
    : null;

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

  const parsedResult = hasResult && result?.content ? parseContentString(result.content) : null;
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
            {functionCallLink ? (
              <Link href={functionCallLink} className="text-blue-500 hover:underline">
                {call.id}
              </Link>
            ) : (
              call.id
            )}
          </div>
        </CardTitle>
        <div className="flex justify-center items-center text-xs">
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
      </CardContent>
    </Card>
  );
};

export default AgentCallDisplay;

