"use client";
import { useMemo, useState, useEffect } from "react";
import ChatGroup from "./SessionGroup";
import type { Session } from "@/types";
import { isToday, isYesterday } from "date-fns";
import { EmptyState } from "./EmptyState";
import { deleteSession, getSessionTasks, createSession } from "@/app/actions/sessions";
import { getAgentHarnessSessionStatus } from "@/app/actions/agentHarnessSession";
import type { SessionActorState } from "@/components/sidebars/ChatItem";
import { formatA2AClientError } from "@/lib/a2aErrors";
import type { SandboxChatMode } from "@/lib/sandboxAgentForm";
import { Button } from "@/components/ui/button";
import { PlusCircle } from "lucide-react";
import { toast } from "sonner";

/** How often the sidebar refreshes harness session actor states. */
const HARNESS_STATUS_POLL_MS = 12000;

interface GroupedChatsProps {
  agentName: string;
  agentNamespace: string;
  sessions: Session[];
  acpSessions?: Array<{ sessionId: string; title?: string; updatedAt?: string }>;
  onAcpSessionClick?: (sessionId: string) => void;
  chatMode?: SandboxChatMode;
  /** When true, this is a substrate AgentHarness: show per-session actor state. */
  isHarness?: boolean;
}

export default function GroupedChats({
  agentName,
  agentNamespace,
  sessions,
  acpSessions = [],
  chatMode = "default",
  isHarness = false,
}: GroupedChatsProps) {
  const hideNewChat = false;
  const hideSessionDelete = false;
  const provisionSessionOnNewChat = chatMode === "multi-session";

  // Local state to manage sessions for immediate UI updates
  const [localSessions, setLocalSessions] = useState<Session[]>(sessions);

  // Per-session substrate actor states (harness sessions only).
  const [sessionStatuses, setSessionStatuses] = useState<Record<string, SessionActorState>>({});

  // Update local sessions when the prop changes
  useEffect(() => {
    setLocalSessions(sessions);
  }, [sessions]);

  // Poll the substrate actor state for each harness session so the sidebar can
  // show a running/suspended indicator that stays in sync with manual actions.
  // Suspending is a harness-wide action and lives in the right (Agent Details)
  // sidebar because the actor is shared by every chat; the
  // "harness-session-suspended" event below keeps these indicators in sync.
  useEffect(() => {
    if (!isHarness) return;
    let cancelled = false;

    const refresh = async () => {
      const ids = sessions.map((s) => s.id).filter((id): id is string => Boolean(id));
      if (ids.length === 0) {
        if (!cancelled) setSessionStatuses({});
        return;
      }
      const results = await Promise.all(
        ids.map(async (id) => {
          const res = await getAgentHarnessSessionStatus(agentNamespace, agentName, id);
          return [id, res.data?.state] as const;
        })
      );
      if (cancelled) return;
      setSessionStatuses((prev) => {
        const next: Record<string, SessionActorState> = {};
        for (const [id, state] of results) {
          next[id] = (state as SessionActorState | undefined) ?? prev[id] ?? "missing";
        }
        return next;
      });
    };

    void refresh();
    const interval = setInterval(() => void refresh(), HARNESS_STATUS_POLL_MS);

    const onSuspended = () => void refresh();
    window.addEventListener("harness-session-suspended", onSuspended);

    return () => {
      cancelled = true;
      clearInterval(interval);
      window.removeEventListener("harness-session-suspended", onSuspended);
    };
  }, [isHarness, sessions, agentNamespace, agentName]);

  const groupedChats = useMemo(() => {
    type SessionWithActivity = {
      session: Session;
      activityTimestamp: number;
    };

    const groups: {
      today: SessionWithActivity[];
      yesterday: SessionWithActivity[];
      older: SessionWithActivity[];
    } = {
      today: [],
      yesterday: [],
      older: [],
    };

    const sessionsWithActivity = localSessions.map(session => ({
      session,
      activityTimestamp: Date.parse(session.updated_at || session.created_at),
    }));

    // Process each session and group by last activity date
    sessionsWithActivity.forEach(sessionWithActivity => {
      const date = new Date(sessionWithActivity.activityTimestamp);
      if (isToday(date)) {
        groups.today.push(sessionWithActivity);
      } else if (isYesterday(date)) {
        groups.yesterday.push(sessionWithActivity);
      } else {
        groups.older.push(sessionWithActivity);
      }
    });

    const sortChats = (sessions: SessionWithActivity[]) =>
      sessions
        .sort((a, b) => b.activityTimestamp - a.activityTimestamp)
        .map(({ session }) => session);

    return {
      today: sortChats(groups.today),
      yesterday: sortChats(groups.yesterday),
      older: sortChats(groups.older),
    };
  }, [localSessions]);

  const onDeleteClick = async (sessionId: string) => {
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

  const onDownloadClick = async (sessionId: string) => {
    toast.promise(
      getSessionTasks(String(sessionId)).then(messages => {
        const blob = new Blob([JSON.stringify(messages, null, 2)], { type: "application/json" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `session-${sessionId}.json`;
        a.click();
        URL.revokeObjectURL(url);
        return messages;
      }),
      {
        loading: "Downloading session...",
        success: "Session downloaded successfully",
        error: "Failed to download session",
      }
    );
  }

  const handleNewChat = async () => {
    // Substrate harness: actor-first ordering. Do NOT pre-create a DB session —
    // its id must be the ACP session id that `session/new` returns. Open the
    // bare chat page with ?new=1 so it starts a fresh chat idle; the first
    // message runs `session/new`, and the chat hook then creates the DB session
    // using that ACP id. (Pre-creating here would mint a random UUID that never
    // matches the actor's ACP session, so the chat could not be stored/resumed.)
    // The full page reload also guarantees the chat hook mounts with clean state.
    if (isHarness) {
      window.location.href = `/agents/${agentNamespace}/${agentName}/chat?new=1`;
      return;
    }
    if (provisionSessionOnNewChat) {
      try {
        const created = await createSession({
          agent_ref: `${agentNamespace}/${agentName}`,
        });
        if (created.error || !created.data) {
          toast.error(formatA2AClientError(created.error ?? "Failed to create session"));
          return;
        }
        const agentRef = `${agentNamespace}/${agentName}`;
        window.dispatchEvent(
          new CustomEvent("new-session-created", {
            detail: { agentRef, session: created.data },
          })
        );
        window.location.href = `/agents/${agentNamespace}/${agentName}/chat/${created.data.id}`;
        return;
      } catch (error) {
        console.error("Error creating session:", error);
        toast.error(formatA2AClientError(error instanceof Error ? error.message : "Failed to create session"));
        return;
      }
    }
    // Force a full page reload instead of client-side navigation
    window.location.href = `/agents/${agentNamespace}/${agentName}/chat`;
  };

  const hasNoSessions = !groupedChats.today.length && !groupedChats.yesterday.length && !groupedChats.older.length && acpSessions.length === 0;

  return (
    <>
      {!hideNewChat && (
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
      )}

      {hasNoSessions || (localSessions.length === 0 && acpSessions.length === 0) ? (
        <EmptyState variant={hideNewChat ? "singleChat" : "default"} />
      ) : (
        <>
          {groupedChats.today.length > 0 && <ChatGroup title="Today" sessions={groupedChats.today} agentName={agentName} agentNamespace={agentNamespace} onDeleteSession={(sessionId) => onDeleteClick(sessionId)} onDownloadSession={(sessionId) => onDownloadClick(sessionId)} hideSessionDelete={hideSessionDelete} sessionStatuses={isHarness ? sessionStatuses : undefined} />}
          {groupedChats.yesterday.length > 0 && (
            <ChatGroup title="Yesterday" sessions={groupedChats.yesterday} agentName={agentName} agentNamespace={agentNamespace} onDeleteSession={(sessionId) => onDeleteClick(sessionId)} onDownloadSession={(sessionId) => onDownloadClick(sessionId)} hideSessionDelete={hideSessionDelete} sessionStatuses={isHarness ? sessionStatuses : undefined} />
          )}
          {groupedChats.older.length > 0 && <ChatGroup title="Older" sessions={groupedChats.older} agentName={agentName} agentNamespace={agentNamespace} onDeleteSession={(sessionId) => onDeleteClick(sessionId)} onDownloadSession={(sessionId) => onDownloadClick(sessionId)} hideSessionDelete={hideSessionDelete} sessionStatuses={isHarness ? sessionStatuses : undefined} />}
        </>
      )}
    </>
  );
}
