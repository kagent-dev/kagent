"use server";

import { AgentMessageConfig, AgentResponse, GetSessionRunsResponse, Message, Run, Session } from "@/types/datamodel";
import { getTeam } from "./teams";
import { createSession, getSession, getSessionRuns, getSessions } from "./sessions";
import { fetchApi, getCurrentUserId, createErrorResponse } from "./utils";
import { getBackendUrl } from "@/lib/utils";

/**
 * Starts a new chat with an agent
 * @param agentId The agent ID
 * @returns A promise with the team, session, and run data
 */
export async function startNewChat(agentId: string): Promise<{ team: AgentResponse; session: Session; }> {
  try {
    const userId = await getCurrentUserId();
    const teamData = await getTeam(agentId);

    if (!teamData.success || !teamData.data) {
      throw new Error("Agent not found");
    }

    // Create new session and run
    const session = await createSession({ user_id: userId, team_id: agentId, name: "New Chat" });
    if (!session.success || !session.data) {
      throw new Error("Failed to create session");
    }

    return { team: teamData.data, session: session.data };
  } catch (error) {
    console.error("Error starting new chat:", error);
    throw error;
  }
}

/**
 * Creates a message for a run
 * @param content The message content
 * @param runId The run ID
 * @param sessionId The session ID
 * @returns A promise with the created message
 */
export async function sendMessage(content: string, sessionId: number): Promise<Message> {
  try {
    const userId = await getCurrentUserId();

    const messageResponse = await fetchApi<Message>(`/sessions/${sessionId}/invoke/stream?user_id=${userId}`, {
      method: "POST",
      body: JSON.stringify({ content }),
    });


    console.log("Message response:", messageResponse);

    return messageResponse;
  } catch (error) {
    console.error("Error sending message:", error);
    throw error;
  }
}

/**
 * Loads an existing chat
 * @param chatId The chat ID
 * @returns A promise with the chat data
 */
export async function loadExistingChat(chatId: string): Promise<{ session: Session; run: Run; team: AgentResponse; messages: Message[] }> {
  try {
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
    if (!teamData.success || !teamData.data) {
      throw new Error("Agent not found");
    }

    return {
      session: sessionData.data,
      run: runData.data[0],
      team: teamData.data,
      messages: runData.data[0].messages || [],
    };
  } catch (error) {
    console.error("Error loading existing chat:", error);
    throw error;
  }
}

/**
 * Gets chat data for an agent
 * @param agentId The agent ID
 * @param chatId The chat ID (optional)
 * @returns A promise with the chat data
 */
export async function getChatData(
  agentId: number, 
  chatId: string | null
): Promise<{ 
  notFound?: boolean; 
  agent?: AgentResponse; 
  sessions?: { session: Session; runs: Run[] }[]; 
  viewState?: { session: Session; run: Run } | null 
}> {
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
        try {
          const { runs } = await fetchApi<GetSessionRunsResponse>(`/sessions/${session.id}/runs`);
          return { session, runs };
        } catch (error) {
          console.error(`Error fetching runs for session ${session.id}:`, error);
          return { session, runs: [] };
        }
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
    console.error("Error getting chat data:", error);
    throw new Error(error instanceof Error ? error.message : "An unexpected error occurred");
  }
}

/* // This Server Action is not suitable for returning a raw Response object for streaming to Client Components.
   // Replaced by a Next.js API Route Handler (app/api/sessions/[sessionId]/invoke/stream/route.ts)
export async function invokeSessionStream(content: string, sessionId: number): Promise<Response> {
  const userId = await getCurrentUserId();
  const backendUrl = getBackendUrl(); 
  const url = `${backendUrl}/sessions/${sessionId}/invoke/stream?user_id=${userId}`;

  const response = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "text/plain", // Changed to text/plain as per user updates
      "Accept": "text/event-stream",
    },
    body: content, // Sending plain text
  });
 
   console.log("Response:", response);
  
  if (!response.ok) {
    let errorMessage = `Request failed with status ${response.status}`;
    try {
      const errorData = await response.json(); 
      if (errorData.error) {
        errorMessage = errorData.error;
      } else if (errorData.message) {
        errorMessage = errorData.message;
      }
    } catch (e) {
      // Ignore
    }
    throw new Error(errorMessage);
  }

  return response; 
}
*/
