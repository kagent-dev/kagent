import type { Session } from "@/types";
import ChatItem from "@/components/sidebars/ChatItem";
import { SidebarGroup, SidebarMenu, SidebarMenuSub } from "../ui/sidebar";
import { Collapsible } from "@radix-ui/react-collapsible";
import { ChevronRight } from "lucide-react";
import { CollapsibleContent, CollapsibleTrigger } from "../ui/collapsible";

interface ChatGroupProps {
  title: string;
  sessions: Session[];
  onDeleteSession: (sessionId: string) => Promise<void>;
  onDownloadSession: (sessionId: string) => Promise<void>;
  agentName: string;
  agentNamespace: string;
}

// The sessions are grouped by today, yesterday, and older
const ChatGroup = ({ title, sessions, onDeleteSession, onDownloadSession, agentName, agentNamespace }: ChatGroupProps) => {
  return (
    <SidebarGroup>
      <SidebarMenu>
        <Collapsible key={title} defaultOpen={title.toLocaleLowerCase() === "today"} className="group/collapsible w-full">
          <div className="w-full">
            <CollapsibleTrigger className="flex items-center justify-between w-full rounded-md p-2 pr-[9px] text-sm hover:bg-sidebar-accent hover:text-sidebar-accent-foreground">
              <span>{title}</span>
              <ChevronRight className="h-4 w-4 shrink-0 transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90" />
            </CollapsibleTrigger>
          </div>
          <CollapsibleContent>
            <SidebarMenuSub className="mx-0 px-0 ml-2 pl-2">
              {sessions.map((session) => (
                <ChatItem key={session.id} sessionId={session.id!} agentName={agentName} agentNamespace={agentNamespace} onDelete={onDeleteSession} sessionName={session.name} onDownload={onDownloadSession} createdAt={session.created_at} />
              ))}
            </SidebarMenuSub>
          </CollapsibleContent>
        </Collapsible>
      </SidebarMenu>
    </SidebarGroup>
  );
};

export default ChatGroup;
