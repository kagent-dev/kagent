"use client";

import { use, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import ChatInterface from "@/components/chat/ChatInterface";
import AcpHarnessChat from "@/components/chat/AcpHarnessChat";
import { getAgentWithResolvedKind } from "@/app/actions/agents";
import { createSession } from "@/app/actions/sessions";
import { isSubstrateSandboxAgent } from "@/lib/sandboxAgentForm";
import { Button } from "@/components/ui/button";
import { Loader2, PlusCircle } from "lucide-react";
import type { Session } from "@/types";

function notifySidebarSession(agentRef: string, session: Session) {
  if (typeof window === "undefined") return;
  window.dispatchEvent(
    new CustomEvent("new-session-created", {
      detail: { agentRef, session },
    })
  );
}

export default function ChatAgentPage({ params }: { params: Promise<{ name: string; namespace: string }> }) {
  const { name, namespace } = use(params);
  const router = useRouter();
  const searchParams = useSearchParams();
  const apcSessionId = searchParams.get("sessionId") || undefined;
  // The sidebar "New Chat" button lands here with ?new=1 to start a fresh
  // harness chat actor-first (no pre-created session).
  const wantNewHarnessChat = searchParams.get("new") === "1";
  const [gate, setGate] = useState<"loading" | "ready">("loading");
  const [harnessSession, setHarnessSession] = useState<{ acpPath: string; sessionId?: string; isNew?: boolean } | null>(null);
  // The harness's /acp WebSocket base, captured once we resolve the agent, so
  // "New Chat" can open a fresh chat without a pre-created session.
  const [harnessAcpBase, setHarnessAcpBase] = useState<string | null>(null);
  // Harness landing: user is on the bare /chat page with no session selected.
  // We don't create a session or start an actor here; the user picks an existing
  // chat from the sidebar or clicks "New Chat" (which creates + opens one).
  const [harnessLanding, setHarnessLanding] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const agentRes = await getAgentWithResolvedKind(name, namespace);
        if (cancelled) return;
        if (agentRes.error || !agentRes.data) {
          setGate("ready");
          return;
        }
        // Substrate AgentHarness: chat over ACP through the controller's
        // same-origin WebSocket proxy instead of the A2A session flow. Each chat
        // session maps to its own substrate actor, keyed by the DB session id.
        const substrateHarness = agentRes.data.substrateAgentHarness;
        if (substrateHarness) {
          const acpBase =
            substrateHarness.acpPath ||
            `/api/agentharnesses/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/acp`;
          setHarnessAcpBase(acpBase);
          // Existing chat opened via ?sessionId= (legacy ACP picker links).
          if (apcSessionId) {
            setHarnessSession({ acpPath: `${acpBase}/${encodeURIComponent(apcSessionId)}`, sessionId: apcSessionId });
            setGate("ready");
            return;
          }
          // "New Chat" from the sidebar lands here with ?new=1: start a fresh
          // chat immediately (actor-first; no pre-created session) instead of
          // showing the landing card. The first message runs session/new and
          // creates the DB session keyed by the ACP id it returns.
          if (wantNewHarnessChat) {
            setHarnessSession({ acpPath: `${acpBase}/new`, isNew: true });
            setGate("ready");
            return;
          }
          // Bare /chat landing: don't create a session, don't spin up an actor,
          // and don't show a spinner. Let the user pick an existing chat from
          // the sidebar or click "New Chat" (which creates + opens one).
          setHarnessLanding(true);
          setGate("ready");
          return;
        }
        if (agentRes.data.workloadMode !== "sandbox") {
          setGate("ready");
          return;
        }
        // Substrate sandbox agents: provision a session up front (same as "New Chat") so the
        // first message uses /chat/:id and does not inline-create + block on readiness polling.
        if (isSubstrateSandboxAgent(agentRes.data)) {
          const created = await createSession({
            agent_ref: `${namespace}/${name}`,
          });
          if (cancelled) return;
          if (!created.error && created.data) {
            notifySidebarSession(`${namespace}/${name}`, created.data);
            router.replace(`/agents/${namespace}/${name}/chat/${created.data.id}`);
            return;
          }
          setGate("ready");
          return;
        }
      } catch {
        /* fall through to chat */
      }
      setGate("ready");
    })();
    return () => {
      cancelled = true;
    };
  }, [name, namespace, router, apcSessionId, wantNewHarnessChat]);

  const startNewHarnessChat = async () => {
    if (!harnessAcpBase) return;
    // Actor-first: don't pre-create a DB session. Open the chat idle; the first
    // message runs session/new, and the ACP id it returns becomes the kagent
    // session id (created then, with the URL swapped to /chat/{id}).
    setHarnessLanding(false);
    setHarnessSession({ acpPath: `${harnessAcpBase}/new`, isNew: true });
  };

  if (gate === "loading") {
    return (
      <div
        className="flex min-h-[50vh] w-full items-center justify-center"
        role="status"
        aria-live="polite"
        aria-busy="true"
      >
        <div className="flex flex-col items-center gap-2">
          <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" aria-hidden />
          <span className="sr-only">Preparing chat…</span>
        </div>
      </div>
    );
  }

  if (harnessSession) {
    return (
      <AcpHarnessChat
        acpPath={harnessSession.acpPath}
        namespace={namespace}
        agentName={name}
        sessionId={harnessSession.sessionId}
        initialLoadSessionId={harnessSession.isNew ? undefined : apcSessionId}
        autoConnect={harnessSession.isNew ? false : undefined}
      />
    );
  }

  if (harnessLanding) {
    return (
      <div className="flex min-h-[60vh] w-full items-center justify-center px-4">
        <div className="max-w-md rounded-lg border bg-card p-8 text-center shadow-sm">
          <h2 className="mb-2 text-lg font-medium">Start chatting</h2>
          <p className="mb-6 text-muted-foreground">
            Pick a conversation from the sidebar, or start a new chat to begin.
          </p>
          <Button onClick={startNewHarnessChat} className="gap-2">
            <PlusCircle className="h-4 w-4" />
            New Chat
          </Button>
        </div>
      </div>
    );
  }

  return <ChatInterface selectedAgentName={name} selectedNamespace={namespace} />;
}
