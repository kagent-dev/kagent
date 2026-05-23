import type { AgentResponse } from "@/types";
import { isHarnessListRow, isOpenshellSandboxRow, isSubstrateHarnessRow } from "@/lib/openshellSandboxAgents";

/**
 * Sandbox CR backends that identify an **agent harness** (declarative harness UX: channels, harness create flow, etc.)
 * as opposed to a generic OpenShell/SSH sandbox row.
 *
 * Extend this union when new harness runtimes are added; pair with UI/server handling for each backend.
 */
export const AGENT_HARNESS_BACKENDS = ["openclaw", "nemoclaw", "hermes"] as const;

export type AgentHarnessBackend = (typeof AGENT_HARNESS_BACKENDS)[number];

export function isAgentHarnessBackend(value: string | undefined | null): value is AgentHarnessBackend {
  return AGENT_HARNESS_BACKENDS.some((b) => b === value);
}

export function getAgentHarnessRuntime(item: AgentResponse): "openshell" | "substrate" | undefined {
  if (!isHarnessListRow(item)) {
    return undefined;
  }
  if (isSubstrateHarnessRow(item)) {
    return "substrate";
  }
  return "openshell";
}

/**
 * When this agent row represents an agent harness, returns the AgentHarness CR backend discriminator (e.g. openclaw vs nemoclaw).
 * Use {@link isAgentHarness} for a simple boolean check.
 */
export function getAgentHarnessBackend(item: AgentResponse): AgentHarnessBackend | undefined {
  if (!isHarnessListRow(item)) {
    return undefined;
  }
  const backend =
    item.substrateAgentHarness?.backend ?? item.openshellAgentHarness?.backend;
  return isAgentHarnessBackend(backend) ? backend : undefined;
}

/** True when the agents-list row is an agent harness. */
export function isAgentHarness(item: AgentResponse): boolean {
  return getAgentHarnessBackend(item) !== undefined;
}

/**
 * Default interactive command when opening the OpenShell terminal for a harness backend.
 * Keep in sync with Go: openclaw.DefaultSSHLaunchCommand / hermes.DefaultSSHLaunchCommand.
 */
export function defaultHarnessSSHLaunchCommand(backend: AgentHarnessBackend): string {
  switch (backend) {
    case "hermes":
      return "cd /sandbox/.hermes && exec hermes";
    case "openclaw":
    case "nemoclaw":
      return "openclaw tui";
    default: {
      const _exhaustive: never = backend;
      return _exhaustive;
    }
  }
}

/** Emoji shown beside harness agents in list/card views. */
export function agentHarnessIcon(backend: AgentHarnessBackend): string {
  switch (backend) {
    case "hermes":
      return "☤";
    case "openclaw":
    case "nemoclaw":
      return "🦞";
    default: {
      const _exhaustive: never = backend;
      return _exhaustive;
    }
  }
}

/** Short label for the agent list “type” column; harness-specific where known. */
export function agentHarnessTypeLabel(backend: AgentHarnessBackend): string {
  switch (backend) {
    case "openclaw":
      return "OpenClaw";
    case "nemoclaw":
      return "NemoClaw";
    case "hermes":
      return "Hermes";
    default: {
      const _exhaustive: never = backend;
      return _exhaustive;
    }
  }
}

export function agentHarnessRuntimeLabel(runtime: "openshell" | "substrate"): string {
  return runtime === "substrate" ? "Substrate" : "OpenShell";
}
