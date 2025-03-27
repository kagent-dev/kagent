"use server";

import { AgentMessageConfig, AgentResponse, GetSessionRunsResponse, Message, Run, Session } from "@/types/datamodel";
import { getTeam } from "./teams";
import { getSession, getSessionRuns, getSessions } from "./sessions";
import { fetchApi, getCurrentUserId } from "./utils";
import { createRunWithSession } from "@/lib/ws";

export async function startNewChat(agentId: string): Promise<{ team: AgentResponse; session: Session; run: Run }> {
  const userId = await getCurrentUserId();
  const teamData = await getTeam(agentId);

  if (!teamData.success || !teamData.data) {
    throw new Error("Agent not found");
  }

  // Create new session and run
  const { session, run } = await createRunWithSession(agentId, userId);
  return { team: teamData.data, session, run };
}

export async function sendMessage(content: string, runId: string, sessionId: number) {
  const userId = await getCurrentUserId();

  const createMessage = (config: AgentMessageConfig, runId: string, sessionId: number, userId: string): Message => ({
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    config,
    session_id: sessionId,
    run_id: runId,
    user_id: userId,
    message_meta: {},
  });

  const message = createMessage({ content, source: "user" }, runId, sessionId, userId);
  return message;
}

export async function loadExistingChat(chatId: string) {
  const sessionData = await getSession(chatId);
  if (!sessionData.success || !sessionData.data) {
    throw new Error("Session not found");
  }

  const runData = await getSessionRuns(chatId);
  if (!runData.success || !runData.data) {
    throw new Error("Run not found");
  }

  // Fetch agent details
  const teamData = await getTeam(String(sessionData.data.team_id));

  return {
    session: sessionData.data,
    run: runData.data[0],
    team: teamData.data,
    messages: runData.data[0].messages || [],
  };
}


export async function getChatData(agentId: number, chatId: string | null): Promise<{ notFound?: boolean; agent?: AgentResponse; sessions?: { session: Session; runs: Run[] }[]; viewState?: { session: Session; run: Run } | null }> {
  try {
    // Fetch agent details
    const agentData = await getTeam(agentId);
    if (!agentData.success || !agentData.data) {
      return { notFound: true, agent: undefined, sessions: undefined, viewState: undefined };
    }

    // Fetch sessions for this agent
    const sessionData = await getSessions();
    if (!sessionData.success || !sessionData.data) {
      return { notFound: true };
    }

    const sessions = sessionData.data;
    const agentSessions = sessions.filter((session) => session.team_id === agentId);

    // Fetch runs for each session
    const sessionsWithRuns = await Promise.all(
      agentSessions.map(async (session) => {
        const { runs } = await fetchApi<GetSessionRunsResponse>(`/sessions/${session.id}/runs`);
        return { session, runs };
      })
    );

    if (chatId) {
      // If we have a chatId, find the specific session
      const session = sessionsWithRuns.find((s) => s.session.id?.toString() === chatId);

      if (!session) {
        return { notFound: true };
      }

      return {
        agent: agentData.data,
        sessions: sessionsWithRuns,
        viewState: {
          session: session.session,
          run: session.runs[0],
        },
      };
    }

    return {
      agent: agentData.data,
      sessions: sessionsWithRuns,
      viewState: null,
    };
  } catch (error) {
    throw new Error(error instanceof Error ? error.message : "An unexpected error occurred");
  }
}
