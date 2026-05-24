import type { ValueSource } from "@/types";
import { k8sRefUtils } from "@/lib/k8sUtils";

/** Default Sandbox CR backend when the harness form does not specify one. */
const SANDBOX_BACKEND_OPENCLAW = "openclaw" as const;

function resolveSandboxBackend(backend?: AgentHarnessSandboxBackend): AgentHarnessSandboxBackend {
  return backend ?? SANDBOX_BACKEND_OPENCLAW;
}

export type AgentHarnessSandboxBackend = "openclaw" | "nemoclaw" | "hermes";

export type SandboxChannelFormType = "telegram" | "slack";

export interface OpenClawChannelRow {
  id: string;
  name: string;
  channelType: SandboxChannelFormType;
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

export function newOpenClawChannelRow(): OpenClawChannelRow {
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
    allowedSlackUserIDs: "",
    slackHomeChannel: "",
    slackHomeChannelName: "",
    interactiveReplies: true,
  };
}

export function isClawHarnessBackend(backend: AgentHarnessSandboxBackend | undefined): boolean {
  return backend === "openclaw" || backend === "nemoclaw";
}

export interface OpenClawSandboxFormSlice {
  /** Optional override for Sandbox.spec.image (OpenShell VM template image). Empty → controller default. */
  image: string;
  channels: OpenClawChannelRow[];
  /**
   * Free-text DNS host list (newline / comma / space separated) that maps to
   * `AgentHarness.spec.network.allowedDomains`. Each host opens an L7 REST endpoint
   * allowing all HTTP methods and paths in the OpenShell sandbox policy; the
   * controller merges these with baseline + channel fragments.
   */
  allowedDomains: string;
}

export function defaultOpenClawSandboxFormSlice(): OpenClawSandboxFormSlice {
  return {
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

/** Where to show a harness OpenClaw validation message and which element to focus. */
export type OpenClawSandboxSectionErrorKind = "allowedDomains" | "channels" | "general";

export interface OpenClawSandboxFormValidationError {
  message: string;
  section: OpenClawSandboxSectionErrorKind;
}

function openClawValidationFail(
  section: OpenClawSandboxSectionErrorKind,
  message: string,
): OpenClawSandboxFormValidationError {
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

/** Client-side validation for OpenClaw Sandbox CR create. */
export function validateOpenClawSandboxForm(args: {
  openClaw: OpenClawSandboxFormSlice;
  modelRef: string | undefined;
  backend?: AgentHarnessSandboxBackend;
}): OpenClawSandboxFormValidationError | undefined {
  const clawBackend = isClawHarnessBackend(resolveSandboxBackend(args.backend));
  const mr = (args.modelRef || "").trim();
  if (!mr) {
    return openClawValidationFail("general", "Please select a model config for this sandbox.");
  }

  for (const entry of trimSplitList(args.openClaw.allowedDomains)) {
    if (!isPlausibleAllowedDomainHost(entry)) {
      return openClawValidationFail(
        "allowedDomains",
        `Allowed domain "${entry}" is not a valid hostname. Use bare DNS names like api.github.com (no scheme or path).`,
      );
    }
  }

  const seenChannelNames = new Set<string>();
  for (const ch of args.openClaw.channels) {
    const cn = ch.name.trim();
    if (!cn) {
      if (
        ch.botToken.trim() ||
        ch.appToken.trim() ||
        (ch.botTokenSource === "secret" && (ch.botSecretName || ch.botSecretKey)) ||
        (ch.appTokenSource === "secret" && (ch.appSecretName || ch.appSecretKey))
      ) {
        return openClawValidationFail("channels", "Each channel with tokens configured needs a binding name.");
      }
      continue;
    }
    if (seenChannelNames.has(cn)) {
      return openClawValidationFail(
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
      return openClawValidationFail("channels", bot.error);
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
        return openClawValidationFail("channels", app.error);
      }
    }

    if (ch.channelType === "slack" && clawBackend) {
      if (ch.channelAccess === "allowlist") {
        const list = trimSplitList(ch.allowlistChannels);
        if (list.length === 0) {
          return openClawValidationFail(
            "channels",
            `Channel "${cn}": allowlist mode requires at least one channel ID.`,
          );
        }
      }
    }
  }

  return undefined;
}

export interface SandboxCRDraft {
  apiVersion: string;
  kind: "AgentHarness";
  metadata: { name: string; namespace: string };
  spec: Record<string, unknown>;
}

function modelConfigRefForSandbox(agentNamespace: string, modelRef: string): string {
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

export function buildSandboxCRDraft(args: {
  name: string;
  namespace: string;
  description: string;
  modelRef: string;
  openClaw: OpenClawSandboxFormSlice;
  backend?: AgentHarnessSandboxBackend;
}): SandboxCRDraft | { error: string } {
  const modelConfigRef = modelConfigRefForSandbox(args.namespace.trim(), args.modelRef);

  const channels: Record<string, unknown>[] = [];

  for (const ch of args.openClaw.channels) {
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
      if (isClawHarnessBackend(resolveSandboxBackend(args.backend))) {
        if (ch.channelAccess !== "open") {
          slack.channelAccess = ch.channelAccess;
        }
        if (ch.channelAccess === "allowlist") {
          slack.allowlistChannels = trimSplitList(ch.allowlistChannels);
        }
        if (!ch.interactiveReplies) {
          slack.interactiveReplies = false;
        }
      } else {
        const allowedSlack = trimSplitList(ch.allowedSlackUserIDs);
        if (allowedSlack.length > 0) {
          slack.allowedUserIDs = allowedSlack;
        }
        const homeChannel = ch.slackHomeChannel.trim();
        if (homeChannel) {
          slack.homeChannel = homeChannel;
          const homeName = ch.slackHomeChannelName.trim();
          if (homeName) {
            slack.homeChannelName = homeName;
          }
        }
      }
      base.slack = slack;
    }

    channels.push(base);
  }

  const backend = resolveSandboxBackend(args.backend);
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

  const img = args.openClaw.image.trim();
  if (img) {
    spec.image = img;
  }

  const allowedDomains = parseAllowedDomainsList(args.openClaw.allowedDomains);
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
