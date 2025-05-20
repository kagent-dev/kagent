import { Session } from "@/types/datamodel";
import ChatItem from "@/components/sidebars/ChatItem";
import { SidebarGroup, SidebarMenu, SidebarMenuButton, SidebarMenuItem, SidebarMenuSub } from "../ui/sidebar";
import { Collapsible } from "@radix-ui/react-collapsible";
import { ChevronRight } from "lucide-react";
import { CollapsibleContent, CollapsibleTrigger } from "../ui/collapsible";

interface ChatGroupProps {
  title: string;
  sessions: Session[];
  onDeleteSession: (sessionId: number) => Promise<void>;
  onDownloadSession: (sessionId: number) => Promise<void>;
  agentId?: number;
}

// The sessions are grouped by today, yesterday, and older
const ChatGroup = ({ title, sessions, onDeleteSession, onDownloadSession, agentId }: ChatGroupProps) => {
  return (
    <SidebarGroup>
      <SidebarMenu>
        <Collapsible key={title} defaultOpen={title.toLocaleLowerCase() === "today"} asChild className="group/collapsible">
          <SidebarMenuItem>
            <CollapsibleTrigger asChild>
              <SidebarMenuButton tooltip={title}>
                <span>{title}</span>
                <ChevronRight className="ml-auto transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90" />
              </SidebarMenuButton>
            </CollapsibleTrigger>
            <CollapsibleContent>
              <SidebarMenuSub>
                {sessions.map((session) => (
                  <ChatItem key={session.id} sessionId={session.id!} agentId={agentId} onDelete={onDeleteSession} sessionName={session.name} onDownload={onDownloadSession} />
                ))}
              </SidebarMenuSub>
            </CollapsibleContent>
          </SidebarMenuItem>
        </Collapsible>
      </SidebarMenu>
    </SidebarGroup>
  );
};

export default ChatGroup;
