import type { AgentResponse } from "@/types";

/**
 * Sandbox CR backends that identify an **agent harness** (declarative harness UX: channels, harness create flow, etc.)
 * as opposed to a generic SSH sandbox row.
 *
 * Extend this union when new harness runtimes are added; pair with UI/server handling for each backend.
 */
export const AGENT_HARNESS_BACKENDS = [
  "openclaw",
  "hermes",
] as const;

export type AgentHarnessBackend = (typeof AGENT_HARNESS_BACKENDS)[number];

export function isAgentHarnessBackend(value: string | undefined | null): value is AgentHarnessBackend {
  return AGENT_HARNESS_BACKENDS.some((b) => b === value);
}

export function getAgentHarnessRuntime(item: AgentResponse): "substrate" | undefined {
  if (!item.substrateAgentHarness) {
    return undefined;
  }
  return "substrate";
}

/**
 * When this agent row represents an OpenClaw harness, returns spec.backend.
 * Other AgentHarness backends are not classified here.
 */
export function getAgentHarnessBackend(item: AgentResponse): AgentHarnessBackend | undefined {
  const backend = item.substrateAgentHarness?.backend;
  return isAgentHarnessBackend(backend) ? backend : undefined;
}

/** True when the agents-list row is an agent harness. */
export function isAgentHarness(item: AgentResponse): boolean {
  return getAgentHarnessBackend(item) !== undefined;
}

/**
 * Default interactive command when opening the Substrate terminal for a harness backend.
 * Keep in sync with Go: openclaw.DefaultSSHLaunchCommand / hermes.DefaultSSHLaunchCommand.
 */
export function defaultHarnessSSHLaunchCommand(backend: AgentHarnessBackend): string {
  switch (backend) {
    case "hermes":
      return "cd /sandbox/.hermes && exec hermes";
    case "openclaw":
      return "openclaw tui";
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
    case "hermes":
      return "Hermes";
    default: {
      const _exhaustive: never = backend;
      return _exhaustive;
    }
  }
}

export function agentHarnessRuntimeLabel(_runtime: "substrate"): string {
  return "Substrate";
}
