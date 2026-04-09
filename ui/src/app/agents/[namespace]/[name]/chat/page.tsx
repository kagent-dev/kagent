"use client";

import { use, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import ChatInterface from "@/components/chat/ChatInterface";
import { getAgent } from "@/app/actions/agents";
import { getSessionsForAgent, createSession } from "@/app/actions/sessions";
import { getCurrentUserId } from "@/app/actions/utils";
import { Loader2 } from "lucide-react";
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
  const [gate, setGate] = useState<"loading" | "ready">("loading");

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const agentRes = await getAgent(name, namespace);
        if (cancelled) return;
        if (agentRes.error || !agentRes.data) {
          setGate("ready");
          return;
        }
        if (!agentRes.data.runInSandbox) {
          setGate("ready");
          return;
        }
        const sessRes = await getSessionsForAgent(namespace, name);
        if (cancelled) return;
        if (sessRes.error || !sessRes.data) {
          setGate("ready");
          return;
        }
        const list = sessRes.data;
        const agentRef = `${namespace}/${name}`;
        if (list.length >= 1) {
          notifySidebarSession(agentRef, list[0]);
          router.replace(`/agents/${namespace}/${name}/chat/${list[0].id}`);
          return;
        }
        const created = await createSession({
          user_id: await getCurrentUserId(),
          agent_ref: agentRef,
          name: "Chat",
        });
        if (cancelled) return;
        if (!created.error && created.data) {
          notifySidebarSession(agentRef, created.data);
          router.replace(`/agents/${namespace}/${name}/chat/${created.data.id}`);
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
  }, [name, namespace, router]);

  if (gate === "loading") {
    return (
      <div className="flex items-center justify-center min-h-[50vh] w-full">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return <ChatInterface selectedAgentName={name} selectedNamespace={namespace} />;
}
