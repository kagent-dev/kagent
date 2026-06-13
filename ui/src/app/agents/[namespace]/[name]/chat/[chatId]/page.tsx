"use client";
import { use, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import ChatInterface from "@/components/chat/ChatInterface";
import AcpHarnessChat from "@/components/chat/AcpHarnessChat";
import { getAgentWithResolvedKind } from "@/app/actions/agents";
import { getSession } from "@/app/actions/sessions";
import { Loader2 } from "lucide-react";

export default function ChatPageView({ params }: { params: Promise<{ name: string; namespace: string; chatId: string }> }) {
  const { name, namespace, chatId } = use(params);
  const searchParams = useSearchParams();
  // A brand-new chat (just created via "New Chat") arrives with ?new=1 and stays
  // idle until the user sends a message; any other navigation (sidebar click,
  // reload) auto-connects and resumes the actor's prior transcript.
  const isNew = searchParams.get("new") === "1";
  const [gate, setGate] = useState<"loading" | "ready">("loading");
  const [harnessAcpPath, setHarnessAcpPath] = useState<string | null>(null);
  const [boundAcpSessionId, setBoundAcpSessionId] = useState<string | undefined>(undefined);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const agentRes = await getAgentWithResolvedKind(name, namespace);
        if (cancelled) return;
        const substrateHarness = agentRes.data?.substrateAgentHarness;
        if (substrateHarness) {
          const acpBase =
            substrateHarness.acpPath ||
            `/api/agentharnesses/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/acp`;
          setHarnessAcpPath(`${acpBase}/${encodeURIComponent(chatId)}`);
          // Reopened chats carry a bound ACP session id (set when the chat first
          // ran session/new); load it so we resume the right conversation inside
          // the harness's shared actor instead of guessing the most recent one.
          if (!isNew) {
            const sessionRes = await getSession(chatId);
            if (!cancelled && sessionRes.data?.acp_session_id) {
              setBoundAcpSessionId(sessionRes.data.acp_session_id);
            }
          }
        }
      } catch {
        /* fall through to the standard chat interface */
      } finally {
        if (!cancelled) setGate("ready");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [name, namespace, chatId, isNew]);

  if (gate === "loading") {
    return (
      <div
        className="flex min-h-[50vh] w-full items-center justify-center"
        role="status"
        aria-live="polite"
        aria-busy="true"
      >
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" aria-hidden />
        <span className="sr-only">Preparing chat…</span>
      </div>
    );
  }

  if (harnessAcpPath) {
    return <AcpHarnessChat acpPath={harnessAcpPath} namespace={namespace} agentName={name} sessionId={chatId} boundAcpSessionId={boundAcpSessionId} autoConnect={!isNew} />;
  }

  return <ChatInterface selectedAgentName={name} selectedNamespace={namespace} sessionId={chatId} />;
}
