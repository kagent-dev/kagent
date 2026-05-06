import type { AgentResponse } from "@/types";
import { isOpenshellSandboxRow } from "@/lib/openshellSandboxAgents";

/**
 * Sandbox CR backends that identify an **agent harness** (declarative harness UX: channels, harness create flow, etc.)
 * as opposed to a generic OpenShell/SSH sandbox row.
 *
 * Extend this union when new harness runtimes are added; pair with UI/server handling for each backend.
 */
export const AGENT_HARNESS_BACKENDS = ["openclaw", "nemoclaw"] as const;

export type AgentHarnessBackend = (typeof AGENT_HARNESS_BACKENDS)[number];

export function isAgentHarnessBackend(value: string | undefined | null): value is AgentHarnessBackend {
  return AGENT_HARNESS_BACKENDS.some((b) => b === value);
}

/**
 * When this agent row represents an agent harness, returns the AgentHarness CR backend discriminator (e.g. openclaw vs nemoclaw).
 * Use {@link isAgentHarness} for a simple boolean check.
 */
export function getAgentHarnessBackend(item: AgentResponse): AgentHarnessBackend | undefined {
  if (!isOpenshellSandboxRow(item)) {
    return undefined;
  }
  const backend = item.openshellAgentHarness?.backend;
  return isAgentHarnessBackend(backend) ? backend : undefined;
}

/** True when the agents-list row is an agent harness (OpenShell sandbox whose backend is a known harness runtime). */
export function isAgentHarness(item: AgentResponse): boolean {
  return getAgentHarnessBackend(item) !== undefined;
}

/** Short label for the agent list “type” column; harness-specific where known. */
export function agentHarnessTypeLabel(backend: AgentHarnessBackend): string {
  switch (backend) {
    case "openclaw":
      return "OpenClaw";
    case "nemoclaw":
      return "NemoClaw";
    default: {
      const _exhaustive: never = backend;
      return _exhaustive;
    }
  }
}
