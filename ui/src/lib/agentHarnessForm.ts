import type { AgentHarnessCrBackend, ValueSource } from "@/types";
import { AGENT_HARNESS_MESSENGER_BACKENDS } from "@/types";
import { k8sRefUtils } from "@/lib/k8sUtils";

/** Matches Kubernetes validation: channels only when backend is openclaw or nemoclaw. */
export function agentHarnessBackendSupportsMessengerChannels(b: AgentHarnessCrBackend): boolean {
  return (AGENT_HARNESS_MESSENGER_BACKENDS as readonly AgentHarnessCrBackend[]).includes(b);
}

export type AgentHarnessChannelFormType = "telegram" | "slack";

export interface AgentHarnessChannelRow {
  id: string;
  name: string;
  channelType: AgentHarnessChannelFormType;
  botTokenSource: "inline" | "secret";
  botToken: string;
  botSecretName: string;
  botSecretKey: string;
  appTokenSource: "inline" | "secret";
  appToken: string;
  appSecretName: string;
  appSecretKey: string;
  channelAccess: "allowlist" | "open" | "disabled";
  allowlistChannels: string;
  allowedUserIDs: string;
  interactiveReplies: boolean;
}

export function newAgentHarnessChannelRow(): AgentHarnessChannelRow {
  return {
    id: crypto.randomUUID(),
    name: "",
    channelType: "telegram",
    botTokenSource: "inline",
    botToken: "",
    botSecretName: "",
    botSecretKey: "",
    appTokenSource: "inline",
    appToken: "",
    appSecretName: "",
    appSecretKey: "",
    channelAccess: "open",
    allowlistChannels: "",
    allowedUserIDs: "",
    interactiveReplies: true,
  };
}

export interface AgentHarnessFormSlice {
  backend: AgentHarnessCrBackend;
  image: string;
  channels: AgentHarnessChannelRow[];
}

export function defaultAgentHarnessFormSlice(): AgentHarnessFormSlice {
  return {
    backend: "openclaw",
    image: "",
    channels: [],
  };
}

function trimSplitList(raw: string): string[] {
  return raw
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter(Boolean);
}

function credentialFromRow(
  source: "inline" | "secret",
  inlineVal: string,
  secretName: string,
  secretKey: string,
  label: string,
): { value?: string; valueFrom?: ValueSource } | { error: string } {
  if (source === "inline") {
    const v = inlineVal.trim();
    if (!v) {
      return { error: `${label}: inline token is required` };
    }
    return { value: v };
  }
  const n = secretName.trim();
  const k = secretKey.trim();
  if (!n || !k) {
    return { error: `${label}: secret name and key are required` };
  }
  return { valueFrom: { type: "Secret", name: n, key: k } };
}

/** Client-side validation for AgentHarness CR create. Returns a single message or undefined. */
export function validateAgentHarnessForm(args: {
  harness: AgentHarnessFormSlice;
  modelRef: string | undefined;
}): string | undefined {
  const mr = (args.modelRef || "").trim();
  if (!mr) {
    return "Please select a model config for this AgentHarness.";
  }

  const channelBackend = agentHarnessBackendSupportsMessengerChannels(args.harness.backend);
  if (!channelBackend && args.harness.channels.length > 0) {
    const hasConfiguredChannel = args.harness.channels.some(
      (ch) =>
        ch.name.trim() ||
        ch.botToken.trim() ||
        ch.appToken.trim() ||
        (ch.botTokenSource === "secret" && (ch.botSecretName || ch.botSecretKey)) ||
        (ch.appTokenSource === "secret" && (ch.appSecretName || ch.appSecretKey)),
    );
    if (hasConfiguredChannel) {
      return "Messenger channels are only supported for OpenClaw and NemoClaw harness types today.";
    }
  }

  for (const ch of args.harness.channels) {
    const cn = ch.name.trim();
    if (!cn) {
      if (
        ch.botToken.trim() ||
        ch.appToken.trim() ||
        (ch.botTokenSource === "secret" && (ch.botSecretName || ch.botSecretKey)) ||
        (ch.appTokenSource === "secret" && (ch.appSecretName || ch.appSecretKey))
      ) {
        return "Each channel with tokens configured needs a binding name.";
      }
      continue;
    }

    const bot = credentialFromRow(
      ch.botTokenSource,
      ch.botToken,
      ch.botSecretName,
      ch.botSecretKey,
      `Channel "${cn}" bot token`,
    );
    if ("error" in bot) {
      return bot.error;
    }

    if (ch.channelType === "slack") {
      const app = credentialFromRow(
        ch.appTokenSource,
        ch.appToken,
        ch.appSecretName,
        ch.appSecretKey,
        `Channel "${cn}" Slack app token`,
      );
      if ("error" in app) {
        return app.error;
      }
    }

    if (ch.channelType === "slack") {
      if (ch.channelAccess === "allowlist") {
        const list = trimSplitList(ch.allowlistChannels);
        if (list.length === 0) {
          return `Channel "${cn}": allowlist mode requires at least one channel ID.`;
        }
      }
    }
  }

  return undefined;
}

export interface AgentHarnessCRDraft {
  apiVersion: string;
  kind: "AgentHarness";
  metadata: { name: string; namespace: string };
  spec: Record<string, unknown>;
}

function modelConfigRefForHarness(agentNamespace: string, modelRef: string): string {
  const t = modelRef.trim();
  if (!t) {
    return "";
  }
  if (k8sRefUtils.isValidRef(t)) {
    const { namespace: ns, name } = k8sRefUtils.fromRef(t);
    if (ns === agentNamespace) {
      return name;
    }
    return `${ns}/${name}`;
  }
  return t;
}

export function buildAgentHarnessCRDraft(args: {
  name: string;
  namespace: string;
  description: string;
  modelRef: string;
  harness: AgentHarnessFormSlice;
}): AgentHarnessCRDraft | { error: string } {
  const modelConfigRef = modelConfigRefForHarness(args.namespace.trim(), args.modelRef);

  const backend = args.harness.backend;

  const channels: Record<string, unknown>[] = [];

  if (agentHarnessBackendSupportsMessengerChannels(args.harness.backend)) {
    for (const ch of args.harness.channels) {
      const cn = ch.name.trim();
      if (!cn) {
        continue;
      }

      const bot = credentialFromRow(
        ch.botTokenSource,
        ch.botToken,
        ch.botSecretName,
        ch.botSecretKey,
        `Channel "${cn}" bot token`,
      );
      if ("error" in bot) {
        return { error: bot.error };
      }

      const base: Record<string, unknown> = {
        name: cn,
        type: ch.channelType,
      };

      if (ch.channelType === "telegram") {
        const allowed = trimSplitList(ch.allowedUserIDs);
        base.telegram = {
          botToken: bot,
          ...(allowed.length > 0 ? { allowedUserIDs: allowed } : {}),
        };
      } else if (ch.channelType === "slack") {
        const app = credentialFromRow(
          ch.appTokenSource,
          ch.appToken,
          ch.appSecretName,
          ch.appSecretKey,
          `Channel "${cn}" Slack app token`,
        );
        if ("error" in app) {
          return { error: app.error };
        }
        const slack: Record<string, unknown> = {
          botToken: bot,
          appToken: app,
          channelAccess: ch.channelAccess,
          ...(ch.channelAccess === "allowlist"
            ? { allowlistChannels: trimSplitList(ch.allowlistChannels) }
            : {}),
        };
        if (!ch.interactiveReplies) {
          slack.interactiveReplies = false;
        }
        base.slack = slack;
      }

      channels.push(base);
    }
  }

  const spec: Record<string, unknown> = {
    backend,
    modelConfigRef,
  };

  const desc = args.description.trim();
  if (desc) {
    spec.description = desc;
  }

  if (channels.length > 0) {
    spec.channels = channels;
  }

  const img = args.harness.image.trim();
  if (img) {
    spec.image = img;
  }

  return {
    apiVersion: "kagent.dev/v1alpha2",
    kind: "AgentHarness",
    metadata: {
      name: args.name.trim(),
      namespace: args.namespace.trim(),
    },
    spec,
  };
}
