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
  sessionId: string;
  onDelete: (sessionId: string) => Promise<void>;
  agentName?: string;
  agentNamespace?: string;
  sessionName?: string;
  onDownload?: (sessionId: string) => Promise<void>;
  createdAt?: string;
}

const ChatItem = ({ sessionId, agentName, agentNamespace, onDelete, sessionName, onDownload, createdAt }: ChatItemProps) => {
  const title = sessionName || "Untitled";
  
  // Format timestamp based on how recent it is
  const formatTime = (dateString?: string) => {
    if (!dateString) return "";

    const date = new Date(dateString);

    const now = new Date();
    const isToday = date.toDateString() === now.toDateString();
    
    // For today: just show time (e.g., "2:30 PM" or "14:30" based on locale)
    if (isToday) {
      return date.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
    }
    
    // For older: show full date and time (e.g., "Nov 28, 2:30 PM" based on locale)
    return date.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ', ' + 
           date.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  };
  
  return (
    <>
      <SidebarMenu>
        <SidebarMenuItem key={sessionId}>
          <SidebarMenuButton asChild className="overflow-hidden relative group/chatitem">
            <Link href={`/agents/${agentNamespace}/${agentName}/chat/${sessionId}`} className="flex items-center w-full">
              <span className="text-sm whitespace-nowrap" title={title}>{title}</span>
              <span className="absolute right-8 top-1/2 -translate-y-1/2 text-xs text-muted-foreground whitespace-nowrap pl-6"
                style={{
                  background: 'linear-gradient(to right, transparent, hsl(var(--sidebar-background)) 30%)',
                }}
              >{formatTime(createdAt)}</span>
            </Link>
          </SidebarMenuButton>
          <DropdownMenu modal={false}>
            <DropdownMenuTrigger asChild>
              <SidebarMenuAction className="!right-2">
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
