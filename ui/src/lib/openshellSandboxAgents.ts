import type { AgentResponse } from "@/types";

export function isOpenshellSandboxRow(item: AgentResponse): boolean {
  return Boolean(item.openshellSandbox?.gatewaySandboxName);
}

export type OpenshellTerminalLinkParams = {
  gatewaySandboxName: string;
  namespace?: string;
  /** Sandbox CR name (Kubernetes metadata.name). */
  crName?: string;
  modelConfigRef?: string;
};

/** Opens `/openshell` with SSH auto-connect (`connect=1`). */
export function openshellTerminalHref(params: OpenshellTerminalLinkParams): string {
  const q = new URLSearchParams({
    sandbox: params.gatewaySandboxName,
    connect: "1",
  });
  const ns = params.namespace?.trim();
  const name = params.crName?.trim();
  const mc = params.modelConfigRef?.trim();
  if (ns) q.set("ns", ns);
  if (name) q.set("name", name);
  if (mc) q.set("modelConfigRef", mc);
  return `/openshell?${q.toString()}`;
}
