import { Run, RunStatus } from "@/types/datamodel";
import {
  AlertDialog,
  AlertDialogTrigger,
  AlertDialogContent,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogCancel,
  AlertDialogAction,
  AlertDialogHeader,
  AlertDialogFooter,
} from "@/components/ui/alert-dialog";
import { AlertCircle, AlertTriangle, CheckCircle, Loader2, MessageSquare, StopCircle, Trash2 } from "lucide-react";
import { Button } from "../ui/button";
import { getRelativeTimeString } from "@/lib/utils";

interface StatusIconProps {
  status: RunStatus;
}

const StatusIcon = ({ status }: StatusIconProps) => {
  switch (status) {
    case "complete":
      return <CheckCircle className="h-4 w-4 text-white/50" />;
    case "active":
    case "created":
      return <Loader2 className="h-4 w-4 text-white/50 animate-spin" />;
    case "stopped":
      return <StopCircle className="h-4 w-4 text-white/50" />;
    case "awaiting_input":
      return <MessageSquare className="h-4 w-4 text-white/50" />;
    case "error":
    case "timeout":
      return <AlertTriangle className="h-4 w-4 text-white/50" />;
    default:
      return <AlertCircle className="h-4 w-4 text-white/50" />;
  }
};

interface RunItemProps {
  sessionId: number;
  run: Run;
  onClick: (sessionId: number, runId: string) => Promise<void>;
  onDelete: (sessionId: number, runId: string) => Promise<void>;
}

const RunItem = ({ sessionId, run, onClick, onDelete }: RunItemProps) => {
  const handleDelete = async () => {
    await onDelete(sessionId, run.id);
  };

  return (
    <div className="group relative">
      <Button onClick={() => onClick(sessionId, run.id)} variant="ghost" className="w-full justify-start text-white/70 hover:text-white hover:bg-transparent gap-2 py-4">
        <StatusIcon status={run.status} />
        <div className="flex flex-col items-start gap-1 min-w-0">
          <span className="truncate max-w-[300px] text-sm text-white/80">{String(run.task?.content || "(new chat)")}</span>
          <div className="inline-flex gap-2 items-center">
            <span className="text-xs text-white/60">{getRelativeTimeString(run.created_at)}</span>
          </div>
        </div>
      </Button>
      <AlertDialog>
        <AlertDialogTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            className="absolute right-2 top-1/2 -translate-y-1/2 opacity-0 group-hover:opacity-100 transition-opacity hover:bg-transparent"
            onClick={(e) => {
              e.stopPropagation();
            }}
          >
            <Trash2 className="h-4 w-4 text-red-400 hover:text-red-300" />
          </Button>
        </AlertDialogTrigger>

        <AlertDialogContent className="bg-[#2A2A2A] border border-[#3A3A3A] text-white">
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Chat</AlertDialogTitle>
            <AlertDialogDescription className="text-white/70">Are you sure you want to delete this chat? This action cannot be undone.</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel className="bg-transparent text-white border-[#3A3A3A] hover:bg-[#3A3A3A] hover:text-white">Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} className="bg-red-500 hover:bg-red-600 text-white">
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
};

export default RunItem;
