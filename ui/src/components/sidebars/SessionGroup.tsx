import { SessionWithRuns } from "@/types/datamodel";
import RunItem from "./RunItem";
import { SidebarGroup, SidebarMenu, SidebarMenuButton, SidebarMenuItem, SidebarMenuSub } from "../ui/sidebar";
import { Collapsible } from "@radix-ui/react-collapsible";
import { ChevronRight } from "lucide-react";
import { CollapsibleContent, CollapsibleTrigger } from "../ui/collapsible";

interface SessionGroupProps {
  title: string;
  sessions: SessionWithRuns[];
  onDeleteSession: (sessionId: number) => Promise<void>;
  agentId?: number;
}

// The sessions are grouped by today, yesterday, and older
const SessionGroup = ({ title, sessions, onDeleteSession, agentId }: SessionGroupProps) => {
  console.log("title", title);
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
                {sessions.map((sessionWithRuns) => (
                  <div key={sessionWithRuns.session.id} className="py-2.5">
                    {sessionWithRuns.runs.map((run) => (
                      <RunItem key={run.id} sessionId={sessionWithRuns.session.id!} agentId={agentId} run={run} onDelete={onDeleteSession} />
                    ))}
                  </div>
                ))}
              </SidebarMenuSub>
            </CollapsibleContent>
          </SidebarMenuItem>
        </Collapsible>
      </SidebarMenu>
    </SidebarGroup>
  );
};

export default SessionGroup;
