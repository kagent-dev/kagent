import type { AgentResponse } from "@/types";
import type { AgentHarnessBackend } from "@/lib/agentHarness";

export function isOpenshellSandboxRow(item: AgentResponse): boolean {
  return Boolean(item.openshellAgentHarness?.gatewaySandboxName);
}

export function isSubstrateHarnessRow(item: AgentResponse): boolean {
  return Boolean(item.substrateAgentHarness?.gatewayUIPath);
}

export function isHarnessListRow(item: AgentResponse): boolean {
  return isOpenshellSandboxRow(item) || isSubstrateHarnessRow(item);
}

export type OpenshellTerminalLinkParams = {
  gatewaySandboxName: string;
  namespace?: string;
  /** Sandbox CR name (Kubernetes metadata.name). */
  crName?: string;
  modelConfigRef?: string;
  /** AgentHarness.spec.backend (openclaw, nemoclaw, hermes). */
  harnessBackend?: AgentHarnessBackend;
};

/** Opens `/openshell` with auto-connect when the page loads (`connect=1`). */
export function openshellTerminalHref(params: OpenshellTerminalLinkParams): string {
  const q = new URLSearchParams({
    sandbox: params.gatewaySandboxName,
    connect: "1",
  });
  if (params.harnessBackend === "openclaw" || params.harnessBackend === "nemoclaw") {
    q.set("clawHarness", "1");
  }
  if (params.harnessBackend) {
    q.set("harnessBackend", params.harnessBackend);
  }
  const ns = params.namespace?.trim();
  const name = params.crName?.trim();
  const mc = params.modelConfigRef?.trim();
  if (ns) q.set("ns", ns);
  if (name) q.set("name", name);
  if (mc) q.set("modelConfigRef", mc);
  return `/openshell?${q.toString()}`;
}
