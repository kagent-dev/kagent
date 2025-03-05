import React, { useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { isToday, isYesterday } from "date-fns";
import { SessionWithRuns, Team } from "@/types/datamodel";
import { SettingsModal } from "@/components/SettingsModal";
import { ActionButtons, EmptyState } from "@/components/sidebars/EmptyState";
import SessionGroup from "@/components/sidebars/SessionGroup";
import { useResponsiveSidebar } from "@/components/sidebars/useResponsiveSidebar";
import KagentLogo from "@/components/kagent-logo";

interface SessionsSidebarProps {
  sessions?: SessionWithRuns[];
  onDeleteSession: (sessionId: number) => Promise<void>;
  onViewRun: (sessionId: number, runId: string) => Promise<void>;
  selectedTeam: Team | null;
}

export default function SessionsSidebar({ sessions = [], selectedTeam, onDeleteSession, onViewRun }: SessionsSidebarProps) {
  const [showSettings, setShowSettings] = useState(false);
  const { isOpen, toggle } = useResponsiveSidebar({ breakpoint: 1024, side: "left" });

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
  const totalSessions = sessions.length;

  return (
    <div
      className={`fixed top-0 z-50 left-0 h-screen transition-all duration-300 ease-in-out 
        bg-[#2A2A2A] border-r border-t border-b border-[#3A3A3A] ${isOpen ? "w-96" : "w-12"}`}
    >
      <div className="h-full flex flex-col text-white">
        {/* Header */}
        <div className="p-4 flex items-center gap-2 border-b border-[#3A3A3A] shrink-0">
          {isOpen && (
            <>
              <h1 className="text-sm font-semibold flex-1">Chat History</h1>
              <span className="text-xs text-white/50">{totalSessions} chats</span>
            </>
          )}
          <Button variant="ghost" size="icon" onClick={toggle} className="h-8 w-8 hover:bg-[#3A3A3A] text-white hover:text-white transition-colors">
            {isOpen ? <ChevronLeft className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
          </Button>
        </div>

        <div className={`flex-1 flex flex-col min-h-0 ${isOpen ? "opacity-100" : "opacity-0"} transition-opacity duration-200`}>
          {!hasNoSessions && (
            <div className="px-4 pt-4 pb-6 shrink-0 border-b border-[#3A3A3A]">
              <ActionButtons hasSessions={!hasNoSessions} currentAgentId={selectedTeam?.id} />
            </div>
          )}

          <ScrollArea className="flex-1 my-4">
            {hasNoSessions ? (
              <EmptyState />
            ) : (
              <div className="space-y-8 p-2">
                {groupedSessions.today.length > 0 && <SessionGroup title="Today" sessions={groupedSessions.today} onViewRun={onViewRun} onDeleteSession={onDeleteSession} />}
                {groupedSessions.yesterday.length > 0 && <SessionGroup title="Yesterday" sessions={groupedSessions.yesterday} onViewRun={onViewRun} onDeleteSession={onDeleteSession} />}
                {groupedSessions.older.length > 0 && <SessionGroup title="Older" sessions={groupedSessions.older} onViewRun={onViewRun} onDeleteSession={onDeleteSession} />}
              </div>
            )}
          </ScrollArea>

          {/* Footer */}
          {isOpen && (
            <>
              {/* <div className="border-t border-[#3A3A3A] p-4 space-y-2 shrink-0">
                <Button variant="ghost" className="w-full justify-start text-white/70 hover:text-white hover:bg-[#3A3A3A] gap-2" onClick={() => setShowSettings(true)}>
                  <Settings className="h-4 w-4" />
                  Settings
                </Button>
                <div className="w-full flex items-center text-sm justify-start text-white/70 hover:text-white gap-2 px-3">
                  <User className="h-4 w-4" />
                  {userId}
                </div>
              </div> */}

              <div className="p-4 border-t border-[#3A3A3A] shrink-0">
                <div className="flex items-center justify-center gap-2 mb-2">
                  <KagentLogo className="h-5 w-5" animate={true} />
                  <span className="font-semibold text-white">kagent.dev</span>
                </div>
                <div className="text-xs text-center text-white/50">© {new Date().getFullYear()} Solo.io. All rights reserved.</div>
              </div>
            </>
          )}
        </div>
        <SettingsModal open={showSettings} onOpenChange={setShowSettings} />
      </div>
    </div>
  );
}
