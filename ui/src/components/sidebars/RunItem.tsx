import { AgentMessageConfig, Run } from "@/types/datamodel";
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
import { MoreHorizontal, Trash2 } from "lucide-react";
import { SidebarMenu, SidebarMenuAction, SidebarMenuButton, SidebarMenuItem } from "@/components/ui/sidebar";
import Link from "next/link";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Button } from "../ui/button";
import { messageUtils } from "@/lib/utils";

interface RunItemProps {
  sessionId: number;
  run: Run;
  onDelete: (sessionId: number, runId: string) => Promise<void>;
  agentId?: number;
}


function  isNestedMessageContent(content: unknown): content is AgentMessageConfig[] {
  if (!Array.isArray(content)) return false;
  return content.every(
    (item) =>
      typeof item === "object" &&
      item !== null &&
      "source" in item &&
      "content" in item &&
      "type" in item
  );
}

const getRunTitle = (run: Run): string => {
  if (run.task) {
    const task = run.task;
    if (typeof task.content === "string" && task.content.trim() !== "") {
      return task.content;
    }
    if (isNestedMessageContent(task.content)) {
      const nested = task.content as AgentMessageConfig[];
      if (nested[0]?.content && typeof nested[0].content === "string" && nested[0].content.trim() !== "") {
        return nested[0].content as string;
      }
    }
  }

  if (run.messages && run.messages.length > 0) {
    const firstUserMessage = run.messages.find(msg => messageUtils.isUserTextMessageContent(msg.config));
    if (firstUserMessage && typeof firstUserMessage.config.content === "string") {
        return firstUserMessage.config.content;
    }
  }

  return "(new chat)";
}

const RunItem = ({ sessionId, run, agentId, onDelete }: RunItemProps) => {
  const title = getRunTitle(run);
  return (
    <>
      <SidebarMenu>
        <SidebarMenuItem key={sessionId}>
          <SidebarMenuButton asChild>
            <Link href={`/agents/${agentId}/chat/${sessionId}`}>
              <span className="text-ellipsis truncate max-w-[100px] text-sm" title={title}>{title}</span>
            </Link>
          </SidebarMenuButton>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <SidebarMenuAction>
                <MoreHorizontal />
                <span className="sr-only">More</span>
              </SidebarMenuAction>
            </DropdownMenuTrigger>
            <DropdownMenuContent>
              <DropdownMenuItem onSelect={(e) => e.preventDefault()} className="p-0">
                <AlertDialog>
                  <AlertDialogTrigger asChild>
                    <Button variant={"ghost"} className="w-full justify-start px-2 py-1.5 text-red-500 hover:text-red-500">
                      <Trash2 className="mr-2 h-4 w-4" />
                      <span>Delete</span>
                    </Button>
                  </AlertDialogTrigger>

                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle className="text-secondary-foreground">Delete Chat</AlertDialogTitle>
                      <AlertDialogDescription>Are you sure you want to delete this chat? This action cannot be undone.</AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel className="text-secondary-foreground">Cancel</AlertDialogCancel>
                      <AlertDialogAction onClick={async () => onDelete(sessionId, run.id)} className="bg-red-500 hover:bg-red-600">
                        Delete
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </SidebarMenuItem>
      </SidebarMenu>
    </>
  );
};

export default RunItem;
