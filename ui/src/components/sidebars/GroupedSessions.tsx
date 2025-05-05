"use client";
import { useMemo } from "react";
import SessionGroup from "./SessionGroup";
import { SessionWithRuns } from "@/types/datamodel";
import { isToday, isYesterday } from "date-fns";
import { EmptyState } from "./EmptyState";
import { deleteSession } from "@/app/actions/sessions";

interface GroupedSessionsProps {
  agentId: number;
  sessionsWithRuns: SessionWithRuns[];
}

export default function GroupedSessions({ agentId, sessionsWithRuns }: GroupedSessionsProps) {
  const sessions = sessionsWithRuns;

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

  const onDeleteClick = async (sessionId: number) => {
    try {
      await deleteSession(sessionId);
    } catch (error) {
      console.error("Error deleting session:", error);
    }
  };

  const hasNoSessions = !groupedSessions.today.length && !groupedSessions.yesterday.length && !groupedSessions.older.length;

  if (hasNoSessions || sessions.length === 0) {
    return <EmptyState />;
  }

  return (
    <>
      {groupedSessions.today.length > 0 && <SessionGroup title="Today" sessions={groupedSessions.today} agentId={agentId} onDeleteSession={(sessionId) => onDeleteClick(sessionId)} />}
      {groupedSessions.yesterday.length > 0 && (
        <SessionGroup title="Yesterday" sessions={groupedSessions.yesterday} agentId={agentId} onDeleteSession={(sessionId) => onDeleteClick(sessionId)} />
      )}
      {groupedSessions.older.length > 0 && <SessionGroup title="Older" sessions={groupedSessions.older} agentId={agentId} onDeleteSession={(sessionId) => onDeleteClick(sessionId)} />}
    </>
  );
}
