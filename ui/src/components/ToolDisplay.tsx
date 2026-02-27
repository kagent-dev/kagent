import { useState } from "react";
import { FunctionCall } from "@/types";
import { ScrollArea } from "@radix-ui/react-scroll-area";
import { FunctionSquare, CheckCircle, Clock, Code, ChevronUp, ChevronDown, Loader2, Text, Check, Copy, AlertCircle, ShieldAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";

export type ToolCallStatus = "requested" | "executing" | "completed" | "pending_approval" | "approved" | "rejected";

interface ToolDisplayProps {
  call: FunctionCall;
  result?: {
    content: string;
    is_error?: boolean;
  };
  status?: ToolCallStatus;
  isError?: boolean;
  onApprove?: () => void;
  onReject?: (reason?: string) => void;
}

const ToolDisplay = ({ call, result, status = "requested", isError = false, onApprove, onReject }: ToolDisplayProps) => {
  const [areArgumentsExpanded, setAreArgumentsExpanded] = useState(status === "pending_approval");
  const [areResultsExpanded, setAreResultsExpanded] = useState(false);
  const [isCopied, setIsCopied] = useState(false);
  const [showRejectInput, setShowRejectInput] = useState(false);
  const [rejectReason, setRejectReason] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);

  const hasResult = result !== undefined;

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(result?.content || "");
      setIsCopied(true);
      setTimeout(() => setIsCopied(false), 2000);
    } catch (err) {
      console.error("Failed to copy text:", err);
    }
  };

  const handleApprove = async () => {
    console.log("[HITL] ToolDisplay.handleApprove called, onApprove defined:", !!onApprove, "status:", status);
    if (!onApprove) {
      console.warn("[HITL] onApprove is undefined - button click ignored");
      return;
    }
    setIsSubmitting(true);
    onApprove();
  };

  const handleReject = async () => {
    console.log("[HITL] ToolDisplay.handleReject called, onReject defined:", !!onReject, "status:", status);
    if (!onReject) {
      console.warn("[HITL] onReject is undefined - button click ignored");
      return;
    }
    setIsSubmitting(true);
    onReject(rejectReason || undefined);
  };

  // Define UI elements based on status
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
      case "pending_approval":
        return (
          <>
            <ShieldAlert className="w-3 h-3 inline-block mr-2 text-amber-500" />
            Approval required
          </>
        );
      case "approved":
        return (
          <>
            <CheckCircle className="w-3 h-3 inline-block mr-2 text-green-500" />
            Approved
          </>
        );
      case "rejected":
        return (
          <>
            <AlertCircle className="w-3 h-3 inline-block mr-2 text-red-500" />
            Rejected
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

  const borderClass = status === "pending_approval"
    ? 'border-amber-300 dark:border-amber-700'
    : status === "rejected"
      ? 'border-red-300 dark:border-red-700'
      : status === "approved"
        ? 'border-green-300 dark:border-green-700'
        : isError
          ? 'border-red-300'
          : '';

  return (
    <Card className={`w-full mx-auto my-1 min-w-full ${borderClass}`}>
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
      <CardContent>
        <div className="space-y-2 mt-4">
          <Button variant="ghost" size="sm" className="p-0 h-auto justify-start" onClick={() => setAreArgumentsExpanded(!areArgumentsExpanded)}>
            <Code className="w-4 h-4 mr-2" />
            <span className="mr-2">Arguments</span>
            {areArgumentsExpanded ? <ChevronUp className="w-4 h-4 ml-auto" /> : <ChevronDown className="w-4 h-4 ml-auto" />}
          </Button>
          {areArgumentsExpanded && (
            <div className="relative">
              <ScrollArea className="max-h-96 overflow-y-auto p-4 w-full mt-2 bg-muted/50">
                <pre className="text-sm whitespace-pre-wrap break-words">
                  {JSON.stringify(call.args, null, 2)}
                </pre>
              </ScrollArea>
            </div>
          )}
        </div>

        {/* Approval buttons */}
        {status === "pending_approval" && !isSubmitting && (
          <div className="mt-4 space-y-2">
            {!showRejectInput ? (
              <div className="flex gap-2">
                <Button
                  size="sm"
                  variant="default"
                  onClick={handleApprove}
                >
                  Approve
                </Button>
                <Button
                  size="sm"
                  variant="destructive"
                  onClick={() => setShowRejectInput(true)}
                >
                  Reject
                </Button>
              </div>
            ) : (
              <div className="space-y-2">
                <Textarea
                  placeholder="Reason for rejection (optional)"
                  value={rejectReason}
                  onChange={(e) => setRejectReason(e.target.value)}
                  className="min-h-[60px] text-sm"
                />
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="destructive"
                    onClick={handleReject}
                  >
                    Confirm Reject
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      setShowRejectInput(false);
                      setRejectReason("");
                    }}
                  >
                    Cancel
                  </Button>
                </div>
              </div>
            )}
          </div>
        )}

        {status === "pending_approval" && isSubmitting && (
          <div className="flex items-center gap-2 py-2 mt-4">
            <Loader2 className="h-4 w-4 animate-spin" />
            <span className="text-sm text-muted-foreground">Submitting decision...</span>
          </div>
        )}

        <div className="mt-4 w-full">
          {status === "executing" && !hasResult && (
            <div className="flex items-center gap-2 py-2">
              <Loader2 className="h-4 w-4 animate-spin" />
              <span className="text-sm">Executing...</span>
            </div>
          )}
          {hasResult && (
            <>
              <Button variant="ghost" size="sm" className="p-0 h-auto justify-start" onClick={() => setAreResultsExpanded(!areResultsExpanded)}>
                <Text className="w-4 h-4 mr-2" />
                <span className="mr-2">{isError ? "Error" : "Results"}</span>
                {areResultsExpanded ? <ChevronUp className="w-4 h-4 ml-auto" /> : <ChevronDown className="w-4 h-4 ml-auto" />}
              </Button>
              {areResultsExpanded && (
                <div className="relative">
                  <ScrollArea className={`max-h-96 overflow-y-auto p-4 w-full mt-2 ${isError ? 'bg-red-50 dark:bg-red-950/10' : ''}`}>
                    <pre className={`text-sm whitespace-pre-wrap break-words ${isError ? 'text-red-600 dark:text-red-400' : ''}`}>
                      {result.content}
                    </pre>
                  </ScrollArea>

                  <Button variant="ghost" size="sm" className="absolute top-2 right-2 p-2" onClick={handleCopy}>
                    {isCopied ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
                  </Button>
                </div>
              )}
            </>
          )}
        </div>
      </CardContent>
    </Card>
  );
};

export default ToolDisplay;
