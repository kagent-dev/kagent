"use client";

import React from "react";
import { ChevronDown, Plus } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { FormSection, FieldRoot, FieldLabel, FieldHint, FieldError } from "@/components/agent-form/form-primitives";
import { cn } from "@/lib/utils";
import type {
  OpenClawChannelRow,
  OpenClawSandboxFormSlice,
  OpenClawSandboxFormValidationError,
} from "@/lib/openClawSandboxForm";
import { newOpenClawChannelRow } from "@/lib/openClawSandboxForm";

const OPENCLAW_DOCS_ROOT = "https://docs.openclaw.ai";

function DocLink({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a href={href} className="text-primary underline-offset-2 hover:underline" target="_blank" rel="noreferrer">
      {children}
    </a>
  );
}

/** Setup guidance aligned with OpenClaw channel wizards (token sources, IDs, access modes). */
function ChannelSetupHint({ title, lines }: { title: string; lines: React.ReactNode[] }) {
  return (
    <div className="rounded-md border border-border/50 bg-muted/25 px-3 py-2.5 text-xs leading-relaxed text-muted-foreground">
      <p className="mb-1.5 font-medium text-foreground/90">{title}</p>
      <ul className="list-disc space-y-1 pl-4">
        {lines.map((line, i) => (
          <li key={i}>{line}</li>
        ))}
      </ul>
    </div>
  );
}

function ChannelTypeSetupHints({ channelType }: { channelType: OpenClawChannelRow["channelType"] }) {
  switch (channelType) {
    case "telegram":
      return (
        <ChannelSetupHint
          title="Telegram setup"
          lines={[
            <>
              <strong>Bot token:</strong> In Telegram, open a chat with{" "}
              <span className="font-mono text-[0.95em]">@BotFather</span>, run <span className="font-mono">/newbot</span> or{" "}
              <span className="font-mono">/mybots</span>, then copy the token (format <span className="font-mono">123456:ABC…</span>
              ).
            </>,
            <>
              Optional: store the token in a Kubernetes Secret and reference it here instead of pasting inline (similar to{" "}
              <span className="font-mono">TELEGRAM_BOT_TOKEN</span> in OpenClaw env-based setup).
            </>,
            <>
              <strong>Allowed user IDs:</strong> Numeric Telegram user ids only (not @usernames). DM your bot first, then read{" "}
              <span className="font-mono">from.id</span> from gateway logs, use Telegram&apos;s{" "}
              <span className="font-mono">getUpdates</span>, or helpers like @userinfobot / @getidsbot. Maps to{" "}
              <span className="font-mono">spec.channels[].telegram.allowedUserIDs</span>. Leave empty to skip an allowlist here.
            </>,
            <>
              More detail: <DocLink href={`${OPENCLAW_DOCS_ROOT}/channels/telegram`}>OpenClaw · Telegram</DocLink>.
            </>,
          ]}
        />
      );
    case "slack":
      return (
        <ChannelSetupHint
          title="Slack setup (Socket Mode)"
          lines={[
            <>
              <strong>Bot token (<span className="font-mono">xoxb-…</span>):</strong> Slack API → your app →{" "}
              <strong>OAuth &amp; Permissions</strong> → <strong>Install to Workspace</strong> (or reinstall) → copy{" "}
              <strong>Bot User OAuth Token</strong>.
            </>,
            <>
              <strong>App-level token (<span className="font-mono">xapp-…</span>):</strong> In the app settings, enable{" "}
              <strong>Socket Mode</strong>, create an app-level token with the connection scope Socket Mode needs, and copy it.
              Both tokens are required for typical Socket Mode bots (same idea as{" "}
              <span className="font-mono">SLACK_BOT_TOKEN</span> + <span className="font-mono">SLACK_APP_TOKEN</span> in OpenClaw).
            </>,
            <>
              Turn on event subscriptions (socket) for messages and app mentions as needed; under <strong>App Home</strong>, enable
              tabs if you rely on DMs/home.
            </>,
            <>
              <strong>Channel access / allowlist:</strong> Use Slack channel IDs (often <span className="font-mono">C…</span> public,{" "}
              <span className="font-mono">G…</span> private multi-person). Right‑click channel → <strong>Copy link</strong> or use{" "}
              <strong>Copy channel ID</strong> if exposed in the client. Maps to{" "}
              <span className="font-mono">spec.channels[].slack.allowlistChannels</span> when access is{" "}
              <span className="font-mono">allowlist</span>.
            </>,
            <>
              <strong>Interactive replies:</strong> When enabled, OpenClaw can use Slack interactive/thread-style replies where
              supported (maps to <span className="font-mono">interactiveReplies</span>).
            </>,
            <>
              More detail: <DocLink href={`${OPENCLAW_DOCS_ROOT}/channels/slack`}>OpenClaw · Slack</DocLink>.
            </>,
          ]}
        />
      );
    default:
      return null;
  }
}

interface OpenClawSandboxFieldsProps {
  value: OpenClawSandboxFormSlice;
  onChange: (next: OpenClawSandboxFormSlice) => void;
  disabled: boolean;
  /** From {@link validateOpenClawSandboxForm}; includes `section` for placement + focus. */
  validationError?: OpenClawSandboxFormValidationError;
}

export function OpenClawSandboxFields({ value, onChange, disabled, validationError }: OpenClawSandboxFieldsProps) {
  const set = (patch: Partial<OpenClawSandboxFormSlice>) => onChange({ ...value, ...patch });
  const [advancedOpen, setAdvancedOpen] = React.useState(false);
  const section = validationError?.section ?? null;

  return (
    <div id="section-openclaw-sandbox" className="space-y-8">
      <FieldError>
        {section === "general" ? validationError?.message : null}
      </FieldError>

      <FormSection
        id="section-openclaw-channels"
        title="Channels integrations"
        description="Optional channel accounts: pick a provider, then credentials (inline or a Kubernetes Secret key in this namespace)."
      >
        <FieldError>{section === "channels" ? validationError?.message : null}</FieldError>

        <FieldRoot>
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <FieldLabel>Channels integrations</FieldLabel>
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={disabled}
              onClick={() => set({ channels: [...value.channels, newOpenClawChannelRow()] })}
            >
              <Plus className="mr-1 h-4 w-4" aria-hidden />
              Add channel
            </Button>
          </div>

          {value.channels.length === 0 ? (
            <p className="text-sm text-muted-foreground">No channels configured.</p>
          ) : (
            <ul className="mt-3 space-y-4">
              {value.channels.map((ch, idx) => (
                <li key={ch.id} className="rounded-lg border border-border/70 bg-card/40 p-4">
                <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                  <span className="text-sm font-medium text-foreground">Channel {idx + 1}</span>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="text-destructive hover:text-destructive"
                    disabled={disabled}
                    onClick={() => set({ channels: value.channels.filter((c) => c.id !== ch.id) })}
                  >
                    Remove
                  </Button>
                </div>
                <div className="grid gap-3 sm:grid-cols-2">
                  <FieldRoot className="space-y-1.5">
                    <FieldLabel className="text-xs" htmlFor={`och-name-${ch.id}`}>
                      Binding name
                    </FieldLabel>
                    <Input
                      id={`och-name-${ch.id}`}
                      value={ch.name}
                      onChange={(e) => {
                        const channels = value.channels.map((c) =>
                          c.id === ch.id ? { ...c, name: e.target.value } : c,
                        );
                        set({ channels });
                      }}
                      placeholder="e.g. team-telegram"
                      disabled={disabled}
                      autoComplete="off"
                    />
                    <FieldHint className="text-[11px]">
                      Stable id for this binding (OpenClaw <span className="font-mono">channels.&lt;provider&gt;.accounts</span> key). Use a
                      short DNS-like label.
                    </FieldHint>
                  </FieldRoot>
                  <FieldRoot className="space-y-1.5">
                    <FieldLabel className="text-xs">Type</FieldLabel>
                    <Select
                      value={ch.channelType}
                      onValueChange={(v) => {
                        const channels = value.channels.map((c) =>
                          c.id === ch.id ? { ...c, channelType: v as OpenClawChannelRow["channelType"] } : c,
                        );
                        set({ channels });
                      }}
                      disabled={disabled}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="telegram">telegram</SelectItem>
                        <SelectItem value="slack">slack</SelectItem>
                      </SelectContent>
                    </Select>
                    <FieldHint className="text-[11px]">
                      Which messenger this row configures. The help panel updates when you change type; existing inputs are left as-is so
                      double‑check tokens match the provider.
                    </FieldHint>
                  </FieldRoot>
                </div>

                <div className="mt-3">
                  <ChannelTypeSetupHints channelType={ch.channelType} />
                </div>

                <div className="mt-3 grid gap-3 sm:grid-cols-2">
                  <FieldRoot className="space-y-1.5">
                    <FieldLabel className="text-xs">
                      {ch.channelType === "slack" ? "Bot token (xoxb-…)" : "Bot token"}
                    </FieldLabel>
                    <Select
                      value={ch.botTokenSource}
                      onValueChange={(v) => {
                        const channels = value.channels.map((c) =>
                          c.id === ch.id ? { ...c, botTokenSource: v as "inline" | "secret" } : c,
                        );
                        set({ channels });
                      }}
                      disabled={disabled}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="inline">Inline value</SelectItem>
                        <SelectItem value="secret">Kubernetes Secret</SelectItem>
                      </SelectContent>
                    </Select>
                    <FieldHint className="text-[11px]">
                      Inline pastes the token into the CR (avoid in shared clusters); Secret references a key in the{" "}
                      <strong>same namespace</strong> as the Sandbox.
                    </FieldHint>
                  </FieldRoot>
                  {ch.botTokenSource === "inline" ? (
                    <FieldRoot className="space-y-1.5">
                      <FieldLabel className="text-xs" htmlFor={`och-bot-${ch.id}`}>
                        Token value
                      </FieldLabel>
                      <Input
                        id={`och-bot-${ch.id}`}
                        type="password"
                        autoComplete="off"
                        value={ch.botToken}
                        onChange={(e) => {
                          const channels = value.channels.map((c) =>
                            c.id === ch.id ? { ...c, botToken: e.target.value } : c,
                          );
                          set({ channels });
                        }}
                        disabled={disabled}
                      />
                    </FieldRoot>
                  ) : (
                    <>
                      <FieldRoot className="space-y-1.5">
                        <FieldLabel className="text-xs">Secret name</FieldLabel>
                        <FieldHint className="text-[11px]">Kubernetes Secret metadata.name in this namespace.</FieldHint>
                        <Input
                          value={ch.botSecretName}
                          onChange={(e) => {
                            const channels = value.channels.map((c) =>
                              c.id === ch.id ? { ...c, botSecretName: e.target.value } : c,
                            );
                            set({ channels });
                          }}
                          disabled={disabled}
                          autoComplete="off"
                        />
                      </FieldRoot>
                      <FieldRoot className="space-y-1.5">
                        <FieldLabel className="text-xs">Secret key</FieldLabel>
                        <Input
                          value={ch.botSecretKey}
                          onChange={(e) => {
                            const channels = value.channels.map((c) =>
                              c.id === ch.id ? { ...c, botSecretKey: e.target.value } : c,
                            );
                            set({ channels });
                          }}
                          disabled={disabled}
                          autoComplete="off"
                        />
                      </FieldRoot>
                    </>
                  )}
                </div>

                {ch.channelType === "slack" && (
                  <>
                    <div className="mt-3 grid gap-3 sm:grid-cols-2">
                      <FieldRoot className="space-y-1.5">
                        <FieldLabel className="text-xs">App-level token (xapp-…)</FieldLabel>
                        <Select
                          value={ch.appTokenSource}
                          onValueChange={(v) => {
                            const channels = value.channels.map((c) =>
                              c.id === ch.id ? { ...c, appTokenSource: v as "inline" | "secret" } : c,
                            );
                            set({ channels });
                          }}
                          disabled={disabled}
                        >
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="inline">Inline value</SelectItem>
                            <SelectItem value="secret">Kubernetes Secret</SelectItem>
                          </SelectContent>
                        </Select>
                        <FieldHint className="text-[11px]">
                          From Slack app settings → <strong>Socket Mode</strong> → generate an app-level token (starts with{" "}
                          <span className="font-mono">xapp-</span>). Required alongside the bot token for Socket Mode.
                        </FieldHint>
                      </FieldRoot>
                      {ch.appTokenSource === "inline" ? (
                        <FieldRoot className="space-y-1.5">
                          <FieldLabel className="text-xs">App token value</FieldLabel>
                          <Input
                            type="password"
                            autoComplete="off"
                            value={ch.appToken}
                            onChange={(e) => {
                              const channels = value.channels.map((c) =>
                                c.id === ch.id ? { ...c, appToken: e.target.value } : c,
                              );
                              set({ channels });
                            }}
                            disabled={disabled}
                          />
                        </FieldRoot>
                      ) : (
                        <>
                          <FieldRoot className="space-y-1.5">
                            <FieldLabel className="text-xs">Secret name</FieldLabel>
                            <Input
                              value={ch.appSecretName}
                              onChange={(e) => {
                                const channels = value.channels.map((c) =>
                                  c.id === ch.id ? { ...c, appSecretName: e.target.value } : c,
                                );
                                set({ channels });
                              }}
                              disabled={disabled}
                              autoComplete="off"
                            />
                          </FieldRoot>
                          <FieldRoot className="space-y-1.5">
                            <FieldLabel className="text-xs">Secret key</FieldLabel>
                            <Input
                              value={ch.appSecretKey}
                              onChange={(e) => {
                                const channels = value.channels.map((c) =>
                                  c.id === ch.id ? { ...c, appSecretKey: e.target.value } : c,
                                );
                                set({ channels });
                              }}
                              disabled={disabled}
                              autoComplete="off"
                            />
                          </FieldRoot>
                        </>
                      )}
                    </div>
                    <div className="mt-3 flex gap-3 rounded-md border border-border/50 bg-muted/15 p-3">
                      <Checkbox
                        id={`och-ir-${ch.id}`}
                        checked={ch.interactiveReplies}
                        onCheckedChange={(checked) => {
                          const channels = value.channels.map((c) =>
                            c.id === ch.id ? { ...c, interactiveReplies: !!checked } : c,
                          );
                          set({ channels });
                        }}
                        disabled={disabled}
                      />
                      <div className="space-y-0.5">
                        <Label htmlFor={`och-ir-${ch.id}`} className="cursor-pointer text-sm font-medium">
                          Slack interactive replies
                        </Label>
                        <p className="text-xs text-muted-foreground">
                          Lets the bot use Slack&apos;s interactive / threaded reply flows where OpenClaw supports them (matches OpenClaw&apos;s{" "}
                          <span className="font-mono">interactiveReplies</span> capability). Turn off if you want a simpler message-only mode.
                          Kubernetes field: <span className="font-mono">spec.channels[].slack.interactiveReplies</span> (defaults to true when
                          omitted).
                        </p>
                      </div>
                    </div>
                  </>
                )}

                {ch.channelType === "slack" && (
                  <div className="mt-3 grid gap-3 sm:grid-cols-2">
                    <FieldRoot className="space-y-1.5">
                      <FieldLabel className="text-xs">Channel access</FieldLabel>
                      <Select
                        value={ch.channelAccess}
                        onValueChange={(v) => {
                          const channels = value.channels.map((c) =>
                            c.id === ch.id ? { ...c, channelAccess: v as OpenClawChannelRow["channelAccess"] } : c,
                          );
                          set({ channels });
                        }}
                        disabled={disabled}
                      >
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="open">open</SelectItem>
                          <SelectItem value="allowlist">allowlist</SelectItem>
                          <SelectItem value="disabled">disabled</SelectItem>
                        </SelectContent>
                      </Select>
                      <FieldHint className="text-[11px]">
                        <span className="font-mono">open</span> — use permitted channels broadly (still respects Slack membership).
                        <span className="font-mono"> allowlist</span> — only listed channel IDs (
                        <span className="font-mono">C…</span>/<span className="font-mono">G…</span>).{" "}
                        <span className="font-mono">disabled</span> — disable routing for this account.
                      </FieldHint>
                    </FieldRoot>
                    {ch.channelAccess === "allowlist" && (
                      <FieldRoot className="space-y-1.5 sm:col-span-2">
                        <FieldLabel className="text-xs">Allowlisted channel IDs</FieldLabel>
                        <FieldHint className="text-[11px]">
                          Slack channel IDs from Copy link / client diagnostics (e.g. C0123… for public channels). Separate with commas or
                          spaces.
                        </FieldHint>
                        <Input
                          value={ch.allowlistChannels}
                          onChange={(e) => {
                            const channels = value.channels.map((c) =>
                              c.id === ch.id ? { ...c, allowlistChannels: e.target.value } : c,
                            );
                            set({ channels });
                          }}
                          placeholder="Comma or space separated"
                          disabled={disabled}
                          autoComplete="off"
                        />
                      </FieldRoot>
                    )}
                  </div>
                )}

                {ch.channelType === "telegram" && (
                  <FieldRoot className="mt-3 space-y-1.5">
                    <FieldLabel className="text-xs">Allowed Telegram user IDs (optional)</FieldLabel>
                    <FieldHint className="text-[11px]">
                      Restrict who can talk to the bot via numeric Telegram user ids (same concept as OpenClaw allowFrom). Leave empty if you
                      are not limiting senders here.
                    </FieldHint>
                    <Input
                      value={ch.allowedUserIDs}
                      onChange={(e) => {
                        const channels = value.channels.map((c) =>
                          c.id === ch.id ? { ...c, allowedUserIDs: e.target.value } : c,
                        );
                        set({ channels });
                      }}
                      placeholder="Comma or space separated numeric IDs"
                      disabled={disabled}
                      autoComplete="off"
                    />
                  </FieldRoot>
                )}
              </li>
            ))}
          </ul>
        )}
      </FieldRoot>
      </FormSection>

      <FormSection
        id="section-openclaw-network"
        title="Network"
        description="Restrict outbound HTTP(S) traffic from the harness to a list of allowed domains. Each entry allows all HTTP methods (GET, POST, PUT, DELETE, …) and all paths on that host."
      >
        <FieldError>{section === "allowedDomains" ? validationError?.message : null}</FieldError>
        <FieldRoot>
          <FieldLabel htmlFor="agent-field-openclaw-allowed-domains">Allowed domains</FieldLabel>
          <FieldHint>
            One host per line (commas and spaces also work). Use bare DNS names like{" "}
            <span className="font-mono">api.github.com</span> or glob labels like{" "}
            <span className="font-mono">*.slack.com</span> — no scheme or path. Domains are merged with the harness baseline and
            channel-derived egress policies.
          </FieldHint>
          <Textarea
            id="agent-field-openclaw-allowed-domains"
            name="allowedDomains"
            value={value.allowedDomains}
            onChange={(e) => set({ allowedDomains: e.target.value })}
            placeholder={"api.github.com\nregistry.npmjs.org\n*.slack.com"}
            rows={4}
            spellCheck={false}
            autoComplete="off"
            disabled={disabled}
            className="font-mono text-sm"
          />
        </FieldRoot>
      </FormSection>

      <section className="rounded-lg border border-border/90 bg-card text-card-foreground shadow-sm">
        <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
          <CollapsibleTrigger
            type="button"
            className="flex w-full items-center justify-between gap-3 border-b border-border/60 px-5 py-4 text-left transition-colors hover:bg-muted/25"
            aria-expanded={advancedOpen}
          >
            <div>
              <h2 className="text-base font-semibold tracking-tight text-foreground">Advanced</h2>
              <p className="mt-1 max-w-2xl text-pretty text-sm leading-relaxed text-muted-foreground">
                Advanced configuration for the sandbox.
              </p>
            </div>
            <ChevronDown
              className={cn("h-5 w-5 shrink-0 text-muted-foreground transition-transform duration-200", advancedOpen && "rotate-180")}
              aria-hidden
            />
          </CollapsibleTrigger>
          <CollapsibleContent>
            <div className="space-y-5 p-5">
              <FieldRoot>
                <FieldLabel htmlFor="agent-field-openclaw-image">Image override</FieldLabel>
                <FieldHint>
                  Overrides the container image for the sandbox VM.
                </FieldHint>
                <Input
                  id="agent-field-openclaw-image"
                  value={value.image}
                  onChange={(e) => set({ image: e.target.value })}
                  className="font-mono text-sm"
                  placeholder="e.g. ghcr.io/kagent-dev/nemoclaw/sandbox-base:2026.5.4"
                  disabled={disabled}
                  autoComplete="off"
                />
              </FieldRoot>
            </div>
          </CollapsibleContent>
        </Collapsible>
      </section>
    </div>
  );
}
