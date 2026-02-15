import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { ShieldAlert } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";

export interface ToolCallInfo {
  name: string;
  args: Record<string, unknown>;
  id: string;
}

interface ToolApprovalDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  toolCalls: ToolCallInfo[];
  onApprove: () => void;
  onReject: () => void;
}

export function ToolApprovalDialog({
  open,
  onOpenChange,
  toolCalls,
  onApprove,
  onReject,
}: ToolApprovalDialogProps) {
  const handleApprove = () => {
    onApprove();
    onOpenChange(false);
  };

  const handleReject = () => {
    onReject();
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader className="flex flex-col items-center sm:items-start gap-2">
          <div className="w-12 h-12 rounded-full bg-amber-100 flex items-center justify-center mb-2">
            <ShieldAlert className="h-6 w-6 text-amber-600" />
          </div>
          <DialogTitle>Tool Approval Required</DialogTitle>
          <DialogDescription>
            {toolCalls.length === 1
              ? "The agent wants to execute the following tool. Do you approve?"
              : `The agent wants to execute ${toolCalls.length} tools. Do you approve?`}
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className="max-h-[300px] mt-2">
          <div className="space-y-3">
            {toolCalls.map((toolCall) => (
              <div
                key={toolCall.id}
                className="rounded-md border bg-muted/50 p-3"
              >
                <div className="font-mono text-sm font-medium mb-1">
                  {toolCall.name}
                </div>
                {Object.keys(toolCall.args).length > 0 && (
                  <pre className="text-xs text-muted-foreground overflow-x-auto whitespace-pre-wrap break-all">
                    {JSON.stringify(toolCall.args, null, 2)}
                  </pre>
                )}
              </div>
            ))}
          </div>
        </ScrollArea>

        <DialogFooter className="sm:justify-end mt-4">
          <div className="flex gap-2 w-full sm:w-auto">
            <Button
              variant="outline"
              onClick={handleReject}
              className="flex-1 sm:flex-initial border-red-200 text-red-600 hover:bg-red-50 hover:text-red-700"
            >
              Reject
            </Button>
            <Button
              onClick={handleApprove}
              className="flex-1 sm:flex-initial"
            >
              Approve
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
