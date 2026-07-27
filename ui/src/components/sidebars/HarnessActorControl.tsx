"use client";

// Harness-level sandbox actor menu shown next to the agent name in the right
// (Agent Details) sidebar. A substrate AgentHarness runs ONE shared actor for
// every chat, so suspend/resume are harness-scoped rather than per-session. The
// (...) menu offers Suspend or Resume depending on the current actor state.

import { useCallback, useState } from "react";
import { Loader2, MoreHorizontal, PauseCircle, PlayCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { toast } from "sonner";
import {
  ensureAgentHarnessSession,
  suspendAgentHarnessSession,
} from "@/app/actions/agentHarnessSession";
import { useHarnessActorStatus } from "@/components/chat/HarnessActorStatusContext";

interface HarnessActorControlProps {
  namespace: string;
  harnessName: string;
  /** Any chat session id of the harness; the actor is shared, so the backend
   * resolves it to the harness's single actor. */
  sessionId: string;
}

export function HarnessActorControl({ namespace, harnessName, sessionId }: HarnessActorControlProps) {
  const actorStatus = useHarnessActorStatus();
  const [busy, setBusy] = useState(false);
  const state = actorStatus?.state;

  const handleSuspend = useCallback(async () => {
    setBusy(true);
    actorStatus?.setState("suspended");
    const res = await suspendAgentHarnessSession(namespace, harnessName, sessionId);
    setBusy(false);
    if (res.error) {
      toast.error(res.error);
      actorStatus?.setState("running");
      return;
    }
    toast.success("Sandbox actor suspended");
  }, [actorStatus, namespace, harnessName, sessionId]);

  const handleResume = useCallback(async () => {
    setBusy(true);
    actorStatus?.setState("running");
    const res = await ensureAgentHarnessSession(namespace, harnessName, sessionId);
    setBusy(false);
    if (res.error) {
      toast.error(res.error);
      actorStatus?.setState("suspended");
      return;
    }
    toast.success("Sandbox actor resumed");
  }, [actorStatus, namespace, harnessName, sessionId]);

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
