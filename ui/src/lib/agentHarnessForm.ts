import type { AgentHarnessCrBackend, ValueSource } from "@/types";
import { AGENT_HARNESS_MESSENGER_BACKENDS } from "@/types";
import { k8sRefUtils } from "@/lib/k8sUtils";
import { generateId } from "@/lib/utils";

/** Matches Kubernetes validation: channels are supported for AgentHarness backends. */
export function agentHarnessBackendSupportsMessengerChannels(b: AgentHarnessCrBackend): boolean {
  return (AGENT_HARNESS_MESSENGER_BACKENDS as readonly AgentHarnessCrBackend[]).includes(b);
}

export function isClawHarnessBackend(backend: AgentHarnessCrBackend | undefined): boolean {
  return backend === "openclaw" || backend === "nemoclaw";
}

export type AgentHarnessChannelFormType = "telegram" | "slack";

export type HarnessRuntimeForm = "openshell" | "substrate";

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
  /** Telegram: maps to spec.channels[].telegram.allowedUserIDs */
  allowedUserIDs: string;
  /** Hermes Slack: maps to spec.channels[].slack.allowedUserIDs → SLACK_ALLOWED_USERS */
  allowedSlackUserIDs: string;
  /** Hermes Slack: maps to spec.channels[].slack.homeChannel → SLACK_HOME_CHANNEL */
  slackHomeChannel: string;
  /** Hermes Slack: maps to spec.channels[].slack.homeChannelName → SLACK_HOME_CHANNEL_NAME */
  slackHomeChannelName: string;
  interactiveReplies: boolean;
}

export function newAgentHarnessChannelRow(): AgentHarnessChannelRow {
  return {
    id: generateId(),
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
    allowedSlackUserIDs: "",
    slackHomeChannel: "",
    slackHomeChannelName: "",
    interactiveReplies: true,
  };
}

export interface AgentHarnessFormSlice {
  backend: AgentHarnessCrBackend;
  /** Harness control plane: OpenShell (default) or Agent Substrate. */
  runtime: HarnessRuntimeForm;
  substrateWorkerPoolRefName: string;
  substrateGatewayToken: string;
  /** GCS snapshot prefix (gs://bucket/path/) — required for generated templates. */
  substrateSnapshotsLocation: string;
  /** Optional override for Sandbox.spec.image (OpenShell VM template image). Empty → controller default. */
  image: string;
  channels: AgentHarnessChannelRow[];
  /**
   * Free-text DNS host list (newline / comma / space separated) that maps to
   * `AgentHarness.spec.network.allowedDomains`. Each host opens an L7 REST endpoint
   * allowing all HTTP methods and paths in the OpenShell sandbox policy; the
   * controller merges these with baseline + channel fragments.
   */
  allowedDomains: string;
}

export function defaultAgentHarnessFormSlice(): AgentHarnessFormSlice {
  return {
    backend: "openclaw",
    runtime: "openshell",
    substrateWorkerPoolRefName: "",
    substrateGatewayToken: "",
    substrateSnapshotsLocation: "gs://ate-snapshots/kagent/",
    image: "",
    channels: [],
    allowedDomains: "",
  };
}

function trimSplitList(raw: string): string[] {
  return raw
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter(Boolean);
}

/**
 * Hostname / glob shape gate for allowedDomains rows. Mirrors what the controller's
 * `NormalizeAllowedDomainHost` will end up storing: bare DNS names, optional `*` /
 * `**` glob labels, no schemes, no paths, no whitespace.
 */
const ALLOWED_DOMAIN_LABEL_RE = /^(\*\*?|[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)$/;

function isPlausibleAllowedDomainHost(raw: string): boolean {
  const s = raw.trim();
  if (!s || s.length > 253) {
    return false;
  }
  if (/[\s/]/.test(s) || s.includes("://")) {
    return false;
  }
  const labels = s.split(".");
  if (labels.length === 0) {
    return false;
  }
  return labels.every((label) => ALLOWED_DOMAIN_LABEL_RE.test(label));
}

/**
 * Splits the textarea contents, dedupes (case-insensitive) and preserves first-seen order.
 * Caller decides whether to send `spec.network.allowedDomains` based on the result length.
 */
export function parseAllowedDomainsList(raw: string): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const entry of trimSplitList(raw)) {
    const key = entry.toLowerCase();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    out.push(entry);
  }
  return out;
}

/** Where to show a harness validation message and which element to focus. */
export type AgentHarnessSectionErrorKind = "allowedDomains" | "channels" | "general";

export interface AgentHarnessFormValidationError {
  message: string;
  section: AgentHarnessSectionErrorKind;
}

function agentHarnessValidationFail(
  section: AgentHarnessSectionErrorKind,
  message: string,
): AgentHarnessFormValidationError {
  return { section, message };
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

/** Client-side validation for AgentHarness CR create. */
export function validateAgentHarnessForm(args: {
  harness: AgentHarnessFormSlice;
  modelRef: string | undefined;
}): AgentHarnessFormValidationError | undefined {
  const backend = args.harness.backend;
  const clawBackend = isClawHarnessBackend(backend);
  const mr = (args.modelRef || "").trim();
  if (!mr) {
    return agentHarnessValidationFail("general", "Please select a model config for this AgentHarness.");
  }
  if (args.harness.runtime === "substrate" && !args.harness.substrateGatewayToken.trim()) {
    return agentHarnessValidationFail("general", "Substrate gateway token is required.");
  }

  for (const entry of trimSplitList(args.harness.allowedDomains)) {
    if (!isPlausibleAllowedDomainHost(entry)) {
      return agentHarnessValidationFail(
        "allowedDomains",
        `Allowed domain "${entry}" is not a valid hostname. Use bare DNS names like api.github.com (no scheme or path).`,
      );
    }
  }

  const channelBackend = agentHarnessBackendSupportsMessengerChannels(backend);
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
      return agentHarnessValidationFail(
        "general",
        "Messenger channels are only supported for OpenClaw and NemoClaw harness types today.",
      );
    }
  }

  const seenChannelNames = new Set<string>();
  for (const ch of args.harness.channels) {
    const cn = ch.name.trim();
    if (!cn) {
      if (
        ch.botToken.trim() ||
        ch.appToken.trim() ||
        (ch.botTokenSource === "secret" && (ch.botSecretName || ch.botSecretKey)) ||
        (ch.appTokenSource === "secret" && (ch.appSecretName || ch.appSecretKey))
      ) {
        return agentHarnessValidationFail("channels", "Each channel with tokens configured needs a binding name.");
      }
      continue;
    }
    if (seenChannelNames.has(cn)) {
      return agentHarnessValidationFail(
        "channels",
        `Duplicate channel binding name "${cn}". Each channel needs a unique name.`,
      );
    }
    seenChannelNames.add(cn);

    const bot = credentialFromRow(
      ch.botTokenSource,
      ch.botToken,
      ch.botSecretName,
      ch.botSecretKey,
      `Channel "${cn}" bot token`,
    );
    if ("error" in bot) {
      return agentHarnessValidationFail("channels", bot.error);
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
        return agentHarnessValidationFail("channels", app.error);
      }
    }

    if (ch.channelType === "slack" && clawBackend) {
      if (ch.channelAccess === "allowlist") {
        const list = trimSplitList(ch.allowlistChannels);
        if (list.length === 0) {
          return agentHarnessValidationFail(
            "channels",
            `Channel "${cn}": allowlist mode requires at least one channel ID.`,
          );
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

  if (agentHarnessBackendSupportsMessengerChannels(backend)) {
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
        };
        if (isClawHarnessBackend(backend)) {
          const openclaw: Record<string, unknown> = {};
          if (ch.channelAccess !== "open") {
            openclaw.channelAccess = ch.channelAccess;
          }
          if (ch.channelAccess === "allowlist") {
            openclaw.allowlistChannels = trimSplitList(ch.allowlistChannels);
          }
          if (!ch.interactiveReplies) {
            openclaw.interactiveReplies = false;
          }
          slack.openclaw = openclaw;
        } else {
          const hermes: Record<string, unknown> = {};
          const allowedSlack = trimSplitList(ch.allowedSlackUserIDs);
          if (allowedSlack.length > 0) {
            hermes.allowedUserIDs = allowedSlack;
          }
          const homeChannel = ch.slackHomeChannel.trim();
          if (homeChannel) {
            hermes.homeChannel = homeChannel;
            const homeName = ch.slackHomeChannelName.trim();
            if (homeName) {
              hermes.homeChannelName = homeName;
            }
          }
          slack.hermes = hermes;
        }
        base.slack = slack;
      }

      channels.push(base);
    }
  }

  const runtime = args.harness.runtime?.trim() || "openshell";

  const spec: Record<string, unknown> = {
    backend,
    runtime,
    modelConfigRef,
  };

  if (runtime === "substrate") {
    const snapshots = args.harness.substrateSnapshotsLocation?.trim();
    if (!snapshots) {
      return { error: "Substrate snapshots location (gs://…) is required." };
    }
    const gatewayToken = args.harness.substrateGatewayToken?.trim();
    if (!gatewayToken) {
      return { error: "Substrate gateway token is required." };
    }
    const substrate: Record<string, unknown> = {
      gatewayToken,
      snapshotsConfig: { location: snapshots },
    };
    const wpName = args.harness.substrateWorkerPoolRefName?.trim();
    if (wpName) {
      substrate.workerPoolRef = {
        name: wpName,
      };
    }
    spec.substrate = substrate;
  }

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

  const allowedDomains = parseAllowedDomainsList(args.harness.allowedDomains);
  if (allowedDomains.length > 0) {
    spec.network = { allowedDomains };
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
