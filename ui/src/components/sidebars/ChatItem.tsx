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
import { MoreHorizontal, Trash2, Download } from "lucide-react";
import { SidebarMenu, SidebarMenuAction, SidebarMenuButton, SidebarMenuItem } from "@/components/ui/sidebar";
import Link from "next/link";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Button } from "../ui/button";

interface ChatItemProps {
  sessionId: number;
  onDelete: (sessionId: number) => Promise<void>;
  agentId?: number;
  sessionName?: string;
  onDownload?: (sessionId: number) => Promise<void>;
}

const ChatItem = ({ sessionId, agentId, onDelete, sessionName, onDownload }: ChatItemProps) => {
  const title = sessionName || "Untitled";
  return (
    <>
      <SidebarMenu>
        <SidebarMenuItem key={sessionId}>
          <SidebarMenuButton asChild>
            <Link href={`/agents/${agentId}/chat/${sessionId}`}>
              <span className="text-ellipsis truncate max-w-[300px] text-sm" title={title}>{title}</span>
            </Link>
          </SidebarMenuButton>
          <DropdownMenu modal={false}>
            <DropdownMenuTrigger asChild>
              <SidebarMenuAction>
                <MoreHorizontal />
                <span className="sr-only">More</span>
              </SidebarMenuAction>
            </DropdownMenuTrigger>
            <DropdownMenuContent>
              <DropdownMenuItem onSelect={(e) => {
                if (onDownload) {
                  onDownload(sessionId);
                } else {
                  e.preventDefault();
                }
              }} className="p-0">
                <Button variant={"ghost"} className="w-full justify-start px-2 py-1.5">
                  <Download className="mr-2 h-4 w-4" />
                  <span>Download</span>
                </Button>
              </DropdownMenuItem>
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
                      <AlertDialogAction onClick={async () => onDelete(sessionId)} className="bg-red-500 hover:bg-red-600">
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

export default ChatItem;
