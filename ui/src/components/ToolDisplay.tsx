import { useState } from "react";
import { FunctionCall } from "@/types";
import { FunctionSquare, CheckCircle, Clock, Code, Loader2, Text, AlertCircle, ShieldAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { SmartContent, parseContentString } from "@/components/chat/SmartContent";
import { CollapsibleSection } from "@/components/chat/CollapsibleSection";

export type ToolCallStatus = "requested" | "executing" | "completed" | "pending_approval" | "approved" | "rejected";

interface ToolDisplayProps {
  call: FunctionCall;
  result?: {
    content: string;
    is_error?: boolean;
  };
  status?: ToolCallStatus;
  isError?: boolean;
  /** When true, the card is in a "decided but not yet submitted" state (batch flow). */
  isDecided?: boolean;
  onApprove?: () => void;
  onReject?: (reason?: string) => void;
}


// ── Main component ─────────────────────────────────────────────────────────
const ToolDisplay = ({ call, result, status = "requested", isError = false, isDecided = false, onApprove, onReject }: ToolDisplayProps) => {
  const [areArgumentsExpanded, setAreArgumentsExpanded] = useState(status === "pending_approval");
  const [areResultsExpanded, setAreResultsExpanded] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [showRejectForm, setShowRejectForm] = useState(false);
  const [rejectionReason, setRejectionReason] = useState("");

  const hasResult = result !== undefined;
  const parsedResult = hasResult ? parseContentString(result.content) : null;

  const handleApprove = async () => {
    if (!onApprove) {
      return;
    }
    setIsSubmitting(true);
    onApprove();
  };

  /** Show the rejection reason form instead of immediately rejecting. */
  const handleRejectClick = () => {
    setShowRejectForm(true);
  };

  /** Confirm rejection — submits with optional reason. */
  const handleRejectConfirm = async () => {
    if (!onReject) {
      return;
    }
    setShowRejectForm(false);
    setIsSubmitting(true);
    onReject(rejectionReason.trim() || undefined);
  };

  /** Cancel the rejection form — go back to Approve/Reject buttons. */
  const handleRejectCancel = () => {
    setShowRejectForm(false);
    setRejectionReason("");
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

  const argsContent = <SmartContent data={call.args} />;
  const resultContent = parsedResult !== null
      ? <SmartContent data={parsedResult} className={isError ? "text-red-600 dark:text-red-400" : ""} />
      : null;

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
        <CollapsibleSection
            icon={Code}
            expanded={areArgumentsExpanded}
            onToggle={() => setAreArgumentsExpanded(!areArgumentsExpanded)}
            previewContent={argsContent}
            expandedContent={argsContent}
        />

        {/* Approval buttons — hidden when decided (batch) or submitting */}
        {status === "pending_approval" && !isSubmitting && !isDecided && !showRejectForm && (
          <div className="mt-4 space-y-2">
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
                onClick={handleRejectClick}
              >
                Reject
              </Button>
            </div>
          </div>
        )}

        {/* Rejection reason form — shown after clicking Reject */}
        {status === "pending_approval" && !isSubmitting && !isDecided && showRejectForm && (
          <div className="mt-4 space-y-2">
            <Textarea
              value={rejectionReason}
              onChange={(e) => setRejectionReason(e.target.value)}
              placeholder="Why are you rejecting this? (optional)"
              className="min-h-[60px] resize-none text-sm"
              autoFocus
            />
            <div className="flex gap-2">
              <Button
                size="sm"
                variant="destructive"
                onClick={handleRejectConfirm}
              >
                Reject
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={handleRejectCancel}
              >
                Cancel
              </Button>
            </div>
          </div>
        )}

        {status === "pending_approval" && (isSubmitting || isDecided) && (
          <div className="flex items-center gap-2 py-2 mt-4">
            <Loader2 className="h-4 w-4 animate-spin" />
            <span className="text-sm text-muted-foreground">
              {isDecided ? "Waiting..." : "Submitting decision..."}
            </span>
          </div>
        )}

        <div className="mt-4 w-full">
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
        </div>
      </CardContent>
    </Card>
  );
};

export default ToolDisplay;
