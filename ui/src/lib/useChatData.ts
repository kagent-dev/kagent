import { useState, useEffect } from "react";
import { Team, Session, Run, GetSessionRunsResponse, SessionWithRuns } from "@/types/datamodel";
import { useRouter } from "next/navigation";
import { fetchApi } from "@/app/actions/utils";

interface UseChatDataProps {
  agentId: string;
  chatId?: string;
}

interface ChatData {
  agent: Team | null;
  sessions: SessionWithRuns[];
  viewState: { session: Session; run: Run } | null;
  isLoading: boolean;
  error: string | null;
}

interface ChatActions {
  handleNewSession: (newSession: Session, newRun?: Run) => Promise<void>;
  handleDeleteSession: (sessionId: number) => Promise<void>;
  handleViewRun: (sessionId: number, runId: string) => Promise<void>;
}

export function useChatData({ agentId, chatId }: UseChatDataProps): [ChatData, ChatActions] {
  const router = useRouter();
  const [data, setData] = useState<ChatData>({
    agent: null,
    sessions: [],
    viewState: null,
    isLoading: true,
    error: null,
  });

  useEffect(() => {
    const fetchData = async () => {
      try {
        setData((prev) => ({ ...prev, isLoading: true, error: null }));

        // Fetch agent details
        const agentData = await fetchApi<Team>(`/teams/${agentId}`);

        // Always fetch sessions for this agent
        const response = await fetchApi<Session[]>(`/sessions`);
        const agentSessions = response.filter((session) => session.team_id === parseInt(agentId));

        // Fetch runs for each session
        const sessionsWithRuns = await Promise.all(
          agentSessions.map(async (session) => {
            const { runs } = await fetchApi<GetSessionRunsResponse>(`/sessions/${session.id}/runs`);
            return { session, runs };
          })
        );

        if (chatId) {
          // If we have a chatId, find the specific session and set viewState
          const session = sessionsWithRuns.find((s) => s.session.id?.toString() === chatId);

          if (!session) {
            router.replace(`/agents/${agentId}/chat`);
            return;
          }

          setData({
            agent: agentData,
            sessions: sessionsWithRuns,
            viewState: {
              session: session.session,
              run: session.runs[0],
            },
            isLoading: false,
            error: null,
          });
        } else {
          // If no chatId, just set the sessions without viewState
          setData({
            agent: agentData,
            sessions: sessionsWithRuns,
            viewState: null,
            isLoading: false,
            error: null,
          });
        }
      } catch (error) {
        console.error("Error fetching data:", error);
        setData((prev) => ({
          ...prev,
          error: error instanceof Error ? error.message : "An unexpected error occurred",
          isLoading: false,
        }));
      }
    };

    fetchData();
  }, [agentId, chatId, router]);

  const actions: ChatActions = {
    handleNewSession: async (newSession: Session, newRun?: Run) => {
      if (!newSession.id) return;

      try {
        // Set loading state before doing anything else
        setData((prev) => ({ ...prev, isLoading: true }));

        // Fetch the runs for the new session
        const { runs } = await fetchApi<GetSessionRunsResponse>(`/sessions/${newSession.id}/runs`);

        setData((prev) => ({
          ...prev,
          sessions: [
            {
              session: newSession,
              runs: newRun ? [newRun, ...runs] : runs,
            },
            ...prev.sessions,
          ],
        }));

        if (!chatId) {
          router.push(`/agents/${agentId}/chat/${newSession.id}`);
        } else {
          setData((prev) => ({ ...prev, isLoading: false }));
        }
      } catch (error) {
        console.error("Error handling new session:", error);
      }
    },

    handleDeleteSession: async (sessionId: number) => {
      try {
        setData((prev) => ({ ...prev, isLoading: true }));

        await fetchApi(`/sessions/${sessionId}`, {
          method: "DELETE",
        });

        setData((prev) => ({
          ...prev,
          sessions: prev.sessions.filter((s) => s.session.id !== sessionId),
          isLoading: false,
        }));

        if (data.viewState?.session.id === sessionId) {
          setData((prev) => ({ ...prev, isLoading: true }));
          router.push(`/agents/${agentId}/chat`);
        }
      } catch (error) {
        console.error("Error deleting session:", error);
        setData((prev) => ({ ...prev, isLoading: false }));
      }
    },

    handleViewRun: async (sessionId: number, runId: string) => {
      const sessionWithRuns = data.sessions.find((s) => s.session.id === sessionId);
      if (!sessionWithRuns) return;

      const run = sessionWithRuns.runs.find((r) => r.id === runId);
      if (!run) return;

      setData((prev) => ({
        ...prev,
        isLoading: true,
        viewState: {
          session: sessionWithRuns.session,
          run,
        },
      }));

      router.push(`/agents/${agentId}/chat/${sessionId}`);
    },
  };

  return [data, actions];
}
