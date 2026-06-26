"use client";

// Harness-level sandbox actor menu shown next to the agent name in the right
// (Agent Details) sidebar. A substrate AgentHarness runs ONE shared actor for
// every chat, so suspend/resume are harness-scoped rather than per-session. The
// (...) menu offers Suspend or Resume depending on the current actor state.

import { useCallback, useEffect, useRef, useState } from "react";
import { Loader2, MoreHorizontal, PauseCircle, PlayCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { toast } from "sonner";
import {
  ensureAgentHarnessSession,
  getAgentHarnessSessionStatus,
  suspendAgentHarnessSession,
  type AgentHarnessSessionState,
} from "@/app/actions/agentHarnessSession";

/** How often the actor state is refreshed while the chat is open. */
const STATUS_POLL_MS = 12000;

interface HarnessActorControlProps {
  namespace: string;
  harnessName: string;
  /** Any chat session id of the harness; the actor is shared, so the backend
   * resolves it to the harness's single actor. */
  sessionId: string;
}

export function HarnessActorControl({ namespace, harnessName, sessionId }: HarnessActorControlProps) {
  const [state, setState] = useState<AgentHarnessSessionState | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const cancelledRef = useRef(false);

  const refresh = useCallback(async () => {
    const res = await getAgentHarnessSessionStatus(namespace, harnessName, sessionId);
    if (cancelledRef.current) return;
    setState((res.data?.state as AgentHarnessSessionState | undefined) ?? "missing");
  }, [namespace, harnessName, sessionId]);

  useEffect(() => {
    cancelledRef.current = false;
    const interval = setInterval(() => void refresh(), STATUS_POLL_MS);
    const onChanged = () => void refresh();
    window.addEventListener("harness-session-suspended", onChanged);
    // Kick the initial fetch from a timer callback rather than synchronously in
    // the effect body, so the first setState lands in a callback (avoids the
    // cascading-render lint and matches the polling/event update paths).
    const initial = setTimeout(() => void refresh(), 0);
    return () => {
      cancelledRef.current = true;
      clearTimeout(initial);
      clearInterval(interval);
      window.removeEventListener("harness-session-suspended", onChanged);
    };
  }, [refresh]);

  const handleSuspend = useCallback(async () => {
    setBusy(true);
    setState("suspended");
    const res = await suspendAgentHarnessSession(namespace, harnessName, sessionId);
    setBusy(false);
    if (res.error) {
      toast.error(res.error);
      setState("running");
      return;
    }
    toast.success("Sandbox actor suspended");
    window.dispatchEvent(new CustomEvent("harness-session-suspended", { detail: { state: "suspended", sessionId } }));
  }, [namespace, harnessName, sessionId]);

  const handleResume = useCallback(async () => {
    setBusy(true);
    setState("running");
    const res = await ensureAgentHarnessSession(namespace, harnessName, sessionId);
    setBusy(false);
    if (res.error) {
      toast.error(res.error);
      setState("suspended");
      return;
    }
    toast.success("Sandbox actor resumed");
    window.dispatchEvent(new CustomEvent("harness-session-suspended", { detail: { state: "running", sessionId } }));
  }, [namespace, harnessName, sessionId]);

  const isRunning = state === "running";
  const canResume = state === "suspended" || state === "missing";

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7 shrink-0"
          aria-label={`Sandbox actor actions for ${harnessName}`}
          disabled={busy || state === undefined}
        >
          {busy ? (
            <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
          ) : (
            <MoreHorizontal className="h-4 w-4" aria-hidden />
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {isRunning ? (
          <DropdownMenuItem onSelect={() => void handleSuspend()}>
            <PauseCircle className="mr-2 h-4 w-4" />
            <span>Suspend</span>
          </DropdownMenuItem>
        ) : (
          <DropdownMenuItem disabled={!canResume} onSelect={() => void handleResume()}>
            <PlayCircle className="mr-2 h-4 w-4" />
            <span>Resume</span>
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export default HarnessActorControl;
