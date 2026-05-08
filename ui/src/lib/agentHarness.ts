import type { AgentHarnessMessengerBackend, AgentResponse } from "@/types";
import { AGENT_HARNESS_MESSENGER_BACKENDS } from "@/types";
import { isOpenshellSandboxRow } from "@/lib/openshellSandboxAgents";

export function isAgentHarnessMessengerBackend(
  value: string | undefined | null,
): value is AgentHarnessMessengerBackend {
  return AGENT_HARNESS_MESSENGER_BACKENDS.some((b) => b === value);
}

/**
 * When this agent row represents an OpenClaw/NemoClaw harness, returns spec.backend.
 * Other AgentHarness backends (e.g. openshell-only rows) are not classified here.
 */
export function getAgentHarnessBackend(item: AgentResponse): AgentHarnessMessengerBackend | undefined {
  if (!isOpenshellSandboxRow(item)) {
    return undefined;
  }
  const backend = item.agentHarness?.backend;
  return isAgentHarnessMessengerBackend(backend) ? backend : undefined;
}

/** True when the agents-list row is a messenger-style harness (openclaw / nemoclaw). */
export function isAgentHarness(item: AgentResponse): boolean {
  return getAgentHarnessBackend(item) !== undefined;
}

/** Short label for the agent list “type” column; harness-specific where known. */
export function agentHarnessTypeLabel(backend: AgentHarnessMessengerBackend): string {
  switch (backend) {
    case "openclaw":
      return "OpenClaw";
    default: {
      const _exhaustive: never = backend;
      return _exhaustive;
    }
  }
}
