"use client";
import { useMemo, useState, useEffect } from "react";
import ChatGroup from "./SessionGroup";
import { Session } from "@/types/datamodel";
import { isToday, isYesterday } from "date-fns";
import { EmptyState } from "./EmptyState";
import { deleteSession } from "@/app/actions/sessions";
import { Button } from "@/components/ui/button";
import { PlusCircle } from "lucide-react";
import { useRouter } from "next/navigation";

interface GroupedChatsProps {
  agentId: number;
  sessions: Session[];
}

export default function GroupedChats({ agentId, sessions }: GroupedChatsProps) {
  const router = useRouter();
  // Local state to manage sessions for immediate UI updates
  const [localSessions, setLocalSessions] = useState<Session[]>(sessions);

  // Update local sessions when the prop changes
  useEffect(() => {
    setLocalSessions(sessions);
  }, [sessions]);

  const groupedChats = useMemo(() => {
    const groups: {
      today: Session[];
      yesterday: Session[];
      older: Session[];
    } = {
      today: [],
      yesterday: [],
      older: [],
    };

    // Process each session and group by date
    localSessions.forEach(session => {
      const date = new Date(session.created_at);
      if (isToday(date)) {
        groups.today.push(session);
      } else if (isYesterday(date)) {
        groups.yesterday.push(session);
      } else {
        groups.older.push(session);
      }
    });

    const sortChats = (sessions: Session[]) =>
      sessions.sort((a, b) => {
        const getLatestTimestamp = (session: Session) => {
          return new Date(session.created_at).getTime();
        };

        return getLatestTimestamp(b) - getLatestTimestamp(a);
      });

    return {
      today: sortChats(groups.today),
      yesterday: sortChats(groups.yesterday),
      older: sortChats(groups.older),
    };
  }, [localSessions]);

  const onDeleteClick = async (sessionId: number) => {
    try {
      // Immediately remove from local state
      setLocalSessions(prev => prev.filter(session => session.id !== sessionId));
      
      // Then delete from server
      await deleteSession(sessionId);
    } catch (error) {
      console.error("Error deleting session:", error);
      // If there's an error, restore the session in the UI
      setLocalSessions(sessions);
    }
  };

  const handleNewChat = () => {
    // Force a full page reload instead of client-side navigation
    window.location.href = `/agents/${agentId}/chat`;
  };

  const hasNoSessions = !groupedChats.today.length && !groupedChats.yesterday.length && !groupedChats.older.length;

  return (
    <>
      <div className="mb-4 px-2">
        <Button
          variant="secondary"
          className="w-full flex items-center justify-center gap-2"
          onClick={handleNewChat}
        >
          <PlusCircle size={16} />
          New Chat
        </Button>
      </div>

      {hasNoSessions || localSessions.length === 0 ? (
        <EmptyState />
      ) : (
        <>
          {groupedChats.today.length > 0 && <ChatGroup title="Today" sessions={groupedChats.today} agentId={agentId} onDeleteSession={(sessionId) => onDeleteClick(sessionId)} />}
          {groupedChats.yesterday.length > 0 && (
            <ChatGroup title="Yesterday" sessions={groupedChats.yesterday} agentId={agentId} onDeleteSession={(sessionId) => onDeleteClick(sessionId)} />
          )}
          {groupedChats.older.length > 0 && <ChatGroup title="Older" sessions={groupedChats.older} agentId={agentId} onDeleteSession={(sessionId) => onDeleteClick(sessionId)} />}
        </>
      )}
    </>
  );
}
