import { useEffect, useRef } from "react";
import type { ChatPhase } from "./useChat";
import { useInvalidateConversations } from "./useInvalidateConversations";

/**
 * Re-reads the conversation lists once when a turn ends.
 *
 * The runtime names a session during its first turn, so the new name exists by the
 * final event but nothing else would fetch it. Not per event: a list refresh mid-turn
 * re-renders the chat and can abort the send in flight.
 */
export function useRefreshConversationsOnTurnEnd(phase: ChatPhase): void {
  const invalidate = useInvalidateConversations();
  const previous = useRef(phase);
  useEffect(() => {
    const ended = previous.current === "streaming" && phase !== "streaming";
    previous.current = phase;
    if (ended) void invalidate();
  }, [phase, invalidate]);
}
