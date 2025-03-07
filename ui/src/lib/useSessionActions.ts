import { useCallback } from "react";
import { useRouter } from "next/navigation";
import { startNewChat } from "@/app/actions/chat";
import { Session, Run } from "@/types/datamodel";

interface UseSessionActionsProps {
  agentId: string;
  handleNewSession?: (newSession: Session, newRun?: Run) => Promise<void>;
}

export function useSessionActions({ agentId, handleNewSession }: UseSessionActionsProps) {
  const router = useRouter();

  const createNewSession = useCallback(async () => {
    try {
      // Use the server action to create a new session
      const { session, run } = await startNewChat(parseInt(agentId));

      // If we have a handler from useChatData, use it
      if (handleNewSession) {
        await handleNewSession(session, run);
      } else {
        // Otherwise just navigate to the new session
        router.push(`/agents/${agentId}/chat/${session.id}`);
      }

      return { session, run };
    } catch (error) {
      console.error("Error creating new session:", error);
      throw error;
    }
  }, [agentId, handleNewSession, router]);

  return { createNewSession };
}
