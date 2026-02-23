import { useState } from "react";
import { FunctionCall } from "@/types";
import { FunctionSquare, CheckCircle, Clock, Code, Loader2, Text, AlertCircle } from "lucide-react";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { SmartContent, parseContentString } from "@/components/chat/SmartContent";
import { CollapsibleSection } from "@/components/chat/CollapsibleSection";

export type ToolCallStatus = "requested" | "executing" | "completed";

interface ToolDisplayProps {
  call: FunctionCall;
  result?: {
    content: string;
    is_error?: boolean;
  };
  status?: ToolCallStatus;
  isError?: boolean;
}


// ── Main component ─────────────────────────────────────────────────────────
const ToolDisplay = ({ call, result, status = "requested", isError = false }: ToolDisplayProps) => {
  const [areArgumentsExpanded, setAreArgumentsExpanded] = useState(false);
  const [areResultsExpanded, setAreResultsExpanded] = useState(false);

  const hasResult = result !== undefined;
  const parsedResult = hasResult ? parseContentString(result.content) : null;

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
            <Clock className="w-3 h-3 inline-block mr-2 text-blue-500" />
            Call requested
          </>
        );
      case "executing":
        return (
          <>
            <Loader2 className="w-3 h-3 inline-block mr-2 text-yellow-500 animate-spin" />
            Executing
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

  const argsContent = <SmartContent data={call.args} />;
  const resultContent = parsedResult !== null
    ? <SmartContent data={parsedResult} className={isError ? "text-red-600 dark:text-red-400" : ""} />
    : null;

  return (
    <Card className={`w-full mx-auto my-1 min-w-full ${isError ? "border-red-300" : ""}`}>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-xs flex space-x-5">
          <div className="flex items-center font-medium">
            <FunctionSquare className="w-4 h-4 mr-2" />
            {call.name}
          </div>
          <div className="font-light">{call.id}</div>
        </CardTitle>
        <div className="flex justify-center items-center text-xs">
          {getStatusDisplay()}
        </div>
      </CardHeader>
      <CardContent className="space-y-1 pt-0">
        <CollapsibleSection
          icon={Code}
          expanded={areArgumentsExpanded}
          onToggle={() => setAreArgumentsExpanded(!areArgumentsExpanded)}
          previewContent={argsContent}
          expandedContent={argsContent}
        />
        {status === "executing" && !hasResult && (
          <div className="flex items-center gap-2 py-1">
            <Loader2 className="h-4 w-4 animate-spin" />
            <span className="text-sm">Executing...</span>
          </div>
        )}
        {hasResult && resultContent && (
          <CollapsibleSection
            icon={Text}
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

export default ToolDisplay;
