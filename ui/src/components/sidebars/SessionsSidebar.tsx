"use client";
import React, { useEffect, useMemo, useState } from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { isToday, isYesterday } from "date-fns";
import { SessionWithRuns, Team } from "@/types/datamodel";
import { SettingsModal } from "@/components/SettingsModal";
import { ActionButtons, EmptyState } from "@/components/sidebars/EmptyState";
import SessionGroup from "@/components/sidebars/SessionGroup";
import { Sidebar, SidebarContent, SidebarHeader, SidebarRail } from "../ui/sidebar";
import { getChatData } from "@/app/actions/chat";
import { LoadingState } from "../LoadingState";
import { deleteSession } from "@/app/actions/sessions";
import { AgentSwitcher } from "./AgentSwitcher";

interface SessionsSidebarProps {
  agentId: string;
}

export default function SessionsSidebar({ agentId }: SessionsSidebarProps) {
  const [sessions, setSessions] = useState<SessionWithRuns[]>([]);
  const [selectedTeam, setSelectedTeam] = useState<Team | null>(null);
  const [showSettings, setShowSettings] = useState(false);

  useEffect(() => {
    const fetchSessions = async () => {
      const data = await getChatData(agentId, null);
      if (data.sessions) {
        setSessions(data.sessions);
      }

      if (data.agent) {
        setSelectedTeam(data.agent);
      }
    };
    fetchSessions();
  }, [agentId]);

  const onDeleteClick = async (sessionId: number) => {
    try {
      await deleteSession(sessionId);
    } catch (error) {
      console.error("Error deleting session:", error);
    } finally {
      const data = await getChatData(agentId, null);
      if (data.sessions) {
        setSessions(data.sessions);
      }
    }
  };

  const groupedSessions = useMemo(() => {
    const groups: {
      today: SessionWithRuns[];
      yesterday: SessionWithRuns[];
      older: SessionWithRuns[];
    } = {
      today: [],
      yesterday: [],
      older: [],
    };

    sessions.forEach((sessionWithRuns) => {
      const mostRecentRun = [...sessionWithRuns.runs].sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())[0];

      if (!mostRecentRun) return;

      const date = new Date(mostRecentRun.created_at);
      if (isToday(date)) {
        groups.today.push(sessionWithRuns);
      } else if (isYesterday(date)) {
        groups.yesterday.push(sessionWithRuns);
      } else {
        groups.older.push(sessionWithRuns);
      }
    });

    const sortSessions = (sessions: SessionWithRuns[]) =>
      sessions.sort((a, b) => {
        const getLatestTimestamp = (session: SessionWithRuns) => {
          const latestRun = [...session.runs].sort((r1, r2) => new Date(r2.created_at).getTime() - new Date(r1.created_at).getTime())[0];
          return latestRun ? new Date(latestRun.created_at).getTime() : 0;
        };

        return getLatestTimestamp(b) - getLatestTimestamp(a);
      });

    return {
      today: sortSessions(groups.today),
      yesterday: sortSessions(groups.yesterday),
      older: sortSessions(groups.older),
    };
  }, [sessions]);

  const hasNoSessions = !groupedSessions.today.length && !groupedSessions.yesterday.length && !groupedSessions.older.length;

  if (!selectedTeam) {
    return <LoadingState />;
  }

  return (
    <Sidebar side="left" collapsible="offcanvas">
      <SidebarHeader>
        <AgentSwitcher selectedTeam={selectedTeam} />      </SidebarHeader>
      <SidebarContent>
        <div className={`flex-1 flex flex-col min-h-0`}>
          {!hasNoSessions && (
            <div className="px-4 pt-4 pb-6 shrink-0 ">
              <ActionButtons hasSessions={!hasNoSessions} currentAgentId={selectedTeam?.id} />
            </div>
          )}

          <ScrollArea className="flex-1 my-4">
            {hasNoSessions ? (
              <EmptyState />
            ) : (
              <>
                {groupedSessions.today.length > 0 && (
                  <SessionGroup title="Today" sessions={groupedSessions.today} agentId={selectedTeam?.id} onDeleteSession={(sessionId) => onDeleteClick(sessionId)} />
                )}
                {groupedSessions.yesterday.length > 0 && (
                  <SessionGroup title="Yesterday" sessions={groupedSessions.yesterday} agentId={selectedTeam?.id} onDeleteSession={(sessionId) => onDeleteClick(sessionId)} />
                )}
                {groupedSessions.older.length > 0 && (
                  <SessionGroup title="Older" sessions={groupedSessions.older} agentId={selectedTeam?.id} onDeleteSession={(sessionId) => onDeleteClick(sessionId)} />
                )}
              </>
            )}
          </ScrollArea>
        </div>
      </SidebarContent>

      <SidebarRail />

      <SettingsModal open={showSettings} onOpenChange={setShowSettings} />
    </Sidebar>
  );
}
