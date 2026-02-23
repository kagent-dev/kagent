"use client";

import { use, useEffect, useState } from "react";
import ChatInterface from "@/components/chat/ChatInterface";
import { findSessionByFunctionCallId } from "@/app/actions/sessions";
import { Loader2 } from "lucide-react";
import Link from "next/link";

export default function FunctionCallPage({
  params,
}: {
  params: Promise<{ namespace: string; name: string; id: string }>;
}) {
  const { namespace, name, id } = use(params);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    async function lookup() {
      const result = await findSessionByFunctionCallId(id);
      if (cancelled) return;
      if (result.data?.id) {
        setSessionId(result.data.id);
      } else {
        setNotFound(true);
      }
      setLoading(false);
    }
    lookup();
    return () => { cancelled = true; };
  }, [id]);

  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[50vh] gap-4">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        <p className="text-sm text-muted-foreground">Looking up sub-agent session…</p>
      </div>
    );
  }

  if (notFound) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[50vh] gap-4">
        <h2 className="text-lg font-medium">Sub-agent session not available yet</h2>
        <p className="text-sm text-muted-foreground">
          The sub-agent may still be starting up. Try refreshing.
        </p>
        <div className="flex gap-3">
          <Link
            href={`/agents/${namespace}/${name}/function-calls/${id}`}
            className="text-sm text-blue-500 hover:underline"
          >
            Retry
          </Link>
          <Link
            href={`/agents/${namespace}/${name}/chat`}
            className="text-sm text-muted-foreground hover:underline"
          >
            Back to chat
          </Link>
        </div>
      </div>
    );
  }

  return (
    <ChatInterface
      selectedAgentName={name}
      selectedNamespace={namespace}
      sessionId={sessionId!}
    />
  );
}
