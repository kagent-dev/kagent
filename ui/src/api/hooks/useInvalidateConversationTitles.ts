import { useInvalidateKeys } from "./useInvalidateKeys";

/* The operation `useConversationTitles` keys its reads under. */
const KEYS = ["conversation-titles"];

/**
 * Re-reads the derived conversation titles on screen, and nothing else.
 *
 * Apart from `useInvalidateConversations` because the chat page calls this when a turn
 * goes idle, where re-reading the conversation list would re-render the page under an
 * in-flight send — see `AgentChatPage`. Only the rows still untitled are read again.
 */
export function useInvalidateConversationTitles(): () => Promise<void> {
  return useInvalidateKeys(KEYS);
}
