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
import { Badge } from "../ui/badge";

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

const getTaskTitle = (task: AgentMessageConfig): string => {
  if (typeof task.content === "string") {
    return task.content;
  }
  if (isNestedMessageContent(task.content)) {

    const nested = task.content as AgentMessageConfig[];
    return nested[0].content as string;
  }
  return "(new chat)";
}

const RunItem = ({ sessionId, run, agentId, onDelete }: RunItemProps) => {
  return (
    <>
      <SidebarMenu>
        <SidebarMenuItem key={sessionId}>
          <SidebarMenuButton asChild>
            <Link href={`/agents/${agentId}/chat/${sessionId}`} className="flex items-center justify-between w-full gap-2">
              <span className="flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-sm">{getTaskTitle(run.task)}</span>
              <Badge variant="outline" className="whitespace-nowrap">{run.status}</Badge>
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
