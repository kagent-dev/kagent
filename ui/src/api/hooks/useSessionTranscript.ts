/**
 * A session's transcript, read once and not written to.
 *
 * This exists for share links, and only for them. Chat itself moved onto
 * `AgentInstance` — the instance is the conversation, and `useChat` is addressed
 * that way — but a share token identifies a *session*, and the tokens already
 * issued still have to resolve. So the two are separate: `useChat` is the live
 * conversation, and this is a record of one that was had.
 *
 * ## Why it is not `useChat` with sending switched off
 *
 * Because `useChat` speaks to the A2A gateway, which routes on the instance
 * headers and knows nothing about sessions. Asking it for a session's history
 * would address the wrong thing. The session's turns come from
 * `SessionService`/`TaskStoreService` instead, through `sessions.tasks`, which
 * reads the stored A2A tasks and hands back messages.
 *
 * ## Why it returns a `ChatController`
 *
 * So `ChatTranscript` renders it. The component takes a controller and this is
 * one, with the three write operations answering immediately and doing nothing:
 * a share is read-only, and the page above never offers a control that would call
 * them. Stubbing them here rather than widening the component's contract keeps
 * "what a transcript needs" in one shape.
 */

import { useEffect, useMemo, useState } from "react";
import { apiClient } from "../client";
import type { ChatMessage } from "../chat/types";
import type { ChatController } from "./useChat";

/** Nothing to do: a shared conversation cannot be written to. */
const NO_OP = async (): Promise<void> => {};
const NO_MESSAGES: ChatMessage[] = [];

export function useSessionTranscript(sessionId: string | undefined): ChatController {
  const [state, setState] = useState<{
    sessionId: string;
    messages?: ChatMessage[];
    error?: Error;
  } | null>(null);

  useEffect(() => {
    if (!sessionId) return;
    const controller = new AbortController();

    apiClient.sessions
      .tasks(sessionId, { signal: controller.signal })
      .then((tasks) => {
        if (controller.signal.aborted) return;
        setState({ sessionId, messages: tasks });
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        setState({
          sessionId,
          error: cause instanceof Error ? cause : new Error(String(cause)),
        });
      });

    return () => controller.abort();
  }, [sessionId]);

  // Tagged with whose transcript it is and derived for the one being viewed, the
  // same way `useChat` does it: cleared by an effect instead, a render would
  // happen first and show the previous conversation under the new one's heading.
  const mine = state?.sessionId === sessionId ? state : null;

  return useMemo(
    () => ({
      messages: mine?.messages ?? NO_MESSAGES,
      isLoadingHistory: Boolean(sessionId) && mine === null,
      historyError: mine?.error,
      turnError: undefined,
      turnState: "idle" as const,
      // A read-only replay has no turn to be in a phase of.
      turnPhase: "idle" as const,
      phase: "idle" as const,
      send: NO_OP,
      cancel: NO_OP,
      dismissQuestion: NO_OP,
      answerQuestion: NO_OP,
      retry: NO_OP,
      // A shared *session* is a record of a conversation that has finished, read
      // through the task store rather than the gateway. Nothing writes to it while it
      // is open, so there is nothing to re-read for.
      refreshTranscript: NO_OP,
    }),
    [mine, sessionId],
  );
}
