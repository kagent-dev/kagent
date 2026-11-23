import { useId, useMemo, useState } from "react";
import { referenceLabel } from "@/api/domain/agentTemplates";
import { Alert, Button, Skeleton, Tooltip, Typography } from "antd";
import { useTheme, type Theme } from "@emotion/react";
import { ArrowUpRight, ChevronRight, FileText, Info } from "lucide-react";
import { useLocation, type To } from "react-router-dom";
import {
  bindingGroupKind,
  groupToolsByServer,
  indexToolServers,
  type ServerToolGroup,
} from "@/api/domain/templateToolCount";
import { conversationHeading } from "@/components/agent-instances/instanceLabels";
import { agentPageUrl } from "@/components/agent/agentUrl";
import { useAgent, useAgentTemplate, useMcpServers, bareName, type AgentInstance, type ObjectReference } from "@/api";
import { ResourceLink } from "@/components/common/ResourceLink";
import { resourceIcons } from "@/components/common/resourceIcons";
import { buildPath, mcpServerTo, paths } from "@/router/routes";
import { backLinkState, type BackLinkState } from "@/router/backLink";
import { ToolTooltip } from "@/components/mcp/ToolTooltip";

const { Text, Paragraph } = Typography;

// region Styles

// Model, three rows of tools and a clamped prompt: the tallest of the live agents measured.
const DETAILS_MIN_HEIGHT = 328;

/** A tool: focusable for its tooltip, but not a link. */
const toolPill = (theme: Theme) =>
  ({
    display: "inline-flex",
    alignItems: "center",
    minHeight: 22,
    padding: "1px 8px",
    borderRadius: 6,
    border: `1px solid ${theme.color.borderStrong}`,
    background: theme.color.bgElevated,
    color: theme.color.text,
    fontFamily: theme.font.mono,
    fontSize: 11.5,
    lineHeight: 1.5,
    wordBreak: "break-word",
    cursor: "default",
    transition: "background-color 120ms ease",
    "&:hover": { background: `${theme.color.text}14` },
    "&:active": { background: `${theme.color.text}24` },
    "&:focus-visible": { outline: `2px solid ${theme.color.textMuted}`, outlineOffset: 2 },
  }) as const;

const hintIcon = (theme: Theme) =>
  ({
    color: theme.color.textMuted,
    cursor: "help",
    flexShrink: 0,
    borderRadius: 999,
    transition: "color 120ms ease",
    "&:hover, &:active": { color: theme.color.text },
    "&:focus-visible": { outline: `2px solid ${theme.color.textMuted}`, outlineOffset: 2 },
  }) as const;

// region Component

/** What this agent is, beside the chat: one card per section. */
export function AgentContextPanel({
  instance,
  autoTitle,
  agentRef,
  onShowDetails,
}: {
  /** A conversation, when there is one open. */
  instance?: AgentInstance;
  /** The title derived from its first message, as the rail shows it. */
  autoTitle?: string;
  /** The agent itself, for surfaces with no conversation open. */
  agentRef?: { namespace: string; name?: string };
  /** Opens the chat details modal. */
  onShowDetails?: () => void;
}) {
  const theme = useTheme();
  const location = useLocation();

  const namespace = instance?.agent?.split("/")[0] ?? agentRef?.namespace ?? "";
  const name = instance?.agent ? bareName(instance.agent) : agentRef?.name;
  const definition = useAgent(namespace, name);
  const templateName = definition.data?.resource.spec.template.ref?.name;
  const template = useAgentTemplate(namespace, templateName);
  const templateNamespace = namespace;
  const harnessRef = definition.data?.resource.spec.harnessRef.name;
  const spec = definition.data?.resource.spec.template.inline ?? template.data?.resource.spec;
  const error = definition.error ?? template.error;
  const loading = definition.isLoading || template.isLoading;
  const tools = spec?.tools ?? [];
  const subAgents = tools.flatMap((binding) => (binding.subAgent ? [binding.subAgent] : []));
  const mcpServers = useMcpServers();
  const serversByRef = useMemo(() => indexToolServers(mcpServers.data ?? []), [mcpServers.data]);
  const back = backLinkState(location, instance ? "Back to conversation" : "Back to agent");
  const linkCss = { justifySelf: "start", fontSize: 12 } as const;
  const agentTo = agentPageUrl({ namespace, name });
  const refPath = (path: string, ref: ObjectReference) =>
    buildPath(path, { namespace: ref.namespace || templateNamespace, name: ref.name });
  const serverTo = (ref: ObjectReference) =>
    mcpServerTo({ namespace: ref.namespace || templateNamespace, name: ref.name, groupKind: bindingGroupKind(ref) });

  return (
    <div
      data-testid="chat-agent-context"
      css={{
        display: "grid",
        gap: theme.space(2),
        alignContent: "start",
        paddingInlineEnd: theme.space(1),
        paddingBlockEnd: theme.space(4),
      }}
    >
      <Field label="Agent" hint="The Agent this chat runs.">
        {agentTo ? (
          <ResourceLink
            mono
            css={linkCss}
            to={agentTo}
            state={back}
            icon={resourceIcons.agent}
            tooltip="Open this agent"
            data-testid="chat-agent-context-agent"
          >
            {`${namespace}/${name}`}
          </ResourceLink>
        ) : (
          <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>Not reported</Text>
        )}
      </Field>

      <Field
        label="Agent template"
        hint={
          definition.data?.resource.spec.template.inline
            ? "This agent has no AgentTemplate: the model, tools and instructions below are written into the agent itself."
            : "The AgentTemplate that sets the model, tools and instructions below. Every agent built from it shares them."
        }
      >
        {templateName && templateNamespace ? (
          <ResourceLink
            mono
            css={linkCss}
            to={buildPath(paths.agentTemplateDetail, { namespace: templateNamespace, name: templateName })}
            state={back}
            icon={resourceIcons.template}
            tooltip="Open the AgentTemplate this agent is built from"
            data-testid="chat-agent-context-template"
          >
            {`${templateNamespace}/${templateName}`}
          </ResourceLink>
        ) : (
          <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
            {definition.data?.resource.spec.template.inline
              ? "None. The agent defines its own model, tools and instructions."
              : "Not reported"}
          </Text>
        )}
      </Field>

      <Field label="Harness" hint="The runtime that executes this agent, such as the built-in kagent engine, Codex or Claude.">
        {harnessRef && namespace ? (
          <ResourceLink
            mono
            css={linkCss}
            to={buildPath(paths.harness, { namespace, name: harnessRef })}
            state={back}
            icon={resourceIcons.harness}
            tooltip="Open the Harness this agent runs on"
            data-testid="chat-agent-context-harness"
          >
            {`${namespace}/${harnessRef}`}
          </ResourceLink>
        ) : (
          <Text data-testid="chat-agent-context-harness" css={{ color: theme.color.textMuted, fontSize: 12 }}>
            Not reported
          </Text>
        )}
      </Field>

      {/* The skeleton holds a typical template's height; the loaded cards take only what they need. */}
      {loading ? (
        <Skeleton
          active
          paragraph={{ rows: 7 }}
          data-testid="chat-agent-context-loading"
          css={{ minHeight: DETAILS_MIN_HEIGHT }}
        />
      ) : error ? (
        <Alert
          type="warning"
          showIcon
          data-testid="chat-agent-context-error"
          title="Could not read this agent's template"
          description={error.message}
        />
      ) : spec ? (
        <div
          data-testid="chat-agent-context-spec"
          css={{ display: "grid", gap: theme.space(2) }}
        >
          <Field label="Model" hint="The ModelConfig that picks the LLM provider and model.">
            {spec.modelConfig?.name ? (
              <ResourceLink
                mono
                css={linkCss}
                to={refPath(paths.modelEdit, spec.modelConfig)}
                state={back}
                icon={resourceIcons.model}
                data-testid="chat-agent-context-model"
              >
                {referenceLabel(spec.modelConfig, templateNamespace)}
              </ResourceLink>
            ) : (
              <Text css={{ fontFamily: theme.font.mono, fontSize: 12 }}>—</Text>
            )}
          </Field>

          <Field label="Tools" hint="The MCP tools and sub-agents the agent can call.">
            {tools.length === 0 ? (
              <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>None — this agent answers from the model alone.</Text>
            ) : (
              <div css={{ display: "grid", gap: theme.space(2) }}>
                {groupToolsByServer(tools, serversByRef, templateNamespace).map((group) => (
                  <ServerTools key={`${group.groupKind}|${group.ref}`} group={group} to={serverTo(group.server)} back={back} />
                ))}
                {subAgents.length ? (
                  <div css={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
                    {subAgents.map((subAgent, index) => (
                      <span key={`${index}-${subAgent.name}`} css={toolPill(theme)} tabIndex={0}>
                        {subAgent.name}
                      </span>
                    ))}
                  </div>
                ) : null}
              </div>
            )}
          </Field>

          {spec.systemPrompt ? (
            <Field label="Instructions" hint="The system prompt the agent starts every chat with.">
              <Paragraph
                data-testid="chat-agent-context-prompt"
                css={{ "&&": { margin: 0 }, fontSize: 12, color: theme.color.textMuted, whiteSpace: "pre-wrap" }}
                ellipsis={{ rows: 8, expandable: true, symbol: "more" }}
              >
                {spec.systemPrompt}
              </Paragraph>
            </Field>
          ) : spec.systemPromptFrom ? (
            <Field label="Instructions" hint="The system prompt the agent starts every chat with.">
              <Text css={{ fontSize: 12, color: theme.color.textMuted }}>
                Read at runtime from the {spec.systemPromptFrom.name} ConfigMap, key {spec.systemPromptFrom.key}.
              </Text>
            </Field>
          ) : null}
        </div>
      ) : null}
      {instance ? (
        <Field label="Chat" hint="This chat's name, ID and prepared revision.">
          <Text
            data-testid="chat-agent-context-session-name"
            ellipsis={{ tooltip: conversationHeading(instance, autoTitle) }}
            css={{ fontSize: 12, color: theme.color.text, minWidth: 0 }}
          >
            {conversationHeading(instance, autoTitle)}
          </Text>
          <Text
            data-testid="chat-agent-context-session"
            copyable={{ text: instance.id, tooltips: ["Copy session ID", "Copied"] }}
            ellipsis={{ tooltip: instance.id }}
            css={{ fontFamily: theme.font.mono, fontSize: 11, color: theme.color.textMuted, minWidth: 0 }}
          >
            {instance.id}
          </Text>
          {instance.preparedRevision ? (
            <span css={{ display: "flex", alignItems: "baseline", gap: theme.space(2), minWidth: 0 }}>
              <Text css={{ fontSize: 11, color: theme.color.textMuted, flexShrink: 0 }}>Revision</Text>
              <Text
                data-testid="chat-agent-context-revision"
                copyable={{ text: instance.preparedRevision, tooltips: ["Copy full revision", "Copied"] }}
                css={{ fontFamily: theme.font.mono, fontSize: 11, color: theme.color.textMuted }}
              >
                {instance.preparedRevision.slice(0, 12)}
              </Text>
            </span>
          ) : null}
          {onShowDetails ? (
            <Button
              size="small"
              block
              icon={<FileText size={14} aria-hidden />}
              onClick={onShowDetails}
              data-testid="chat-agent-context-details"
              css={{ marginTop: theme.space(1), fontSize: 12 }}
            >
              Chat details
            </Button>
          ) : null}
        </Field>
      ) : null}
    </div>
  );
}

/** What `count` says for one server: how many of its tools the agent takes. */
function toolCountLabel({ allTools, selected, discovered }: ServerToolGroup): string {
  const total = discovered?.length;
  const tools = (n: number) => (n === 1 ? "tool" : "tools");
  if (allTools) return total ? `All ${total} ${tools(total)}` : "All tools";
  return total ? `${selected.length}/${total} ${tools(total)}` : `${selected.length} ${tools(selected.length)}`;
}

/** One MCP server's row: toggles its tools from anywhere, with a link to the server in the middle. */
function ServerTools({ group, to, back }: { group: ServerToolGroup; to: To; back: BackLinkState }) {
  const theme = useTheme();
  const [open, setOpen] = useState(false);
  const listId = useId();
  const descriptions = new Map(group.discovered?.map((tool) => [tool.name, tool.description]));
  const names = group.allTools ? (group.discovered ?? []).map((tool) => tool.name) : group.selected;

  return (
    <div
      data-testid="chat-agent-context-server-group"
      css={{
        display: "grid",
        gap: theme.space(2),
        "& + &": { borderTop: `1px solid ${theme.color.border}`, paddingTop: theme.space(2) },
      }}
    >
      {/* The toggle spans the whole row; the link sits above it in the middle column, so neither nests in the other. */}
      <div css={{ display: "grid", gridTemplateColumns: "20px minmax(0, 1fr) auto", margin: "-2px -4px" }}>
        <button
          type="button"
          aria-expanded={open}
          aria-controls={listId}
          aria-label={`${open ? "Hide" : "Show"} tools from ${group.ref}`}
          data-testid="chat-agent-context-server-toggle"
          onClick={() => setOpen((value) => !value)}
          css={{
            gridArea: "1 / 1 / 2 / -1",
            display: "grid",
            gridTemplateColumns: "subgrid",
            alignItems: "center",
            columnGap: theme.space(1),
            padding: "2px 4px",
            border: 0,
            borderRadius: 6,
            background: "transparent",
            color: theme.color.textMuted,
            font: "inherit",
            textAlign: "start",
            cursor: "pointer",
            transition: "background-color 120ms ease",
            "&:hover": { background: `${theme.color.text}0f` },
            "&:active": { background: `${theme.color.text}1f` },
            "&:focus-visible": { outline: `2px solid ${theme.color.textMuted}`, outlineOffset: 0 },
          }}
        >
          <ChevronRight
            size={14}
            aria-hidden
            css={{ justifySelf: "center", transition: "transform 150ms ease", transform: open ? "rotate(90deg)" : undefined }}
          />
          <span
            data-testid="chat-agent-context-server-count"
            css={{
              gridColumn: 3,
              paddingInlineStart: theme.space(2),
              fontSize: 12,
              lineHeight: "20px",
              fontVariantNumeric: "tabular-nums",
              whiteSpace: "nowrap",
            }}
          >
            {toolCountLabel(group)}
          </span>
        </button>
        <ResourceLink
          mono
          icon={resourceIcons.mcpServer}
          to={to}
          state={back}
          tooltip="Open this MCP server"
          data-testid="chat-agent-context-server"
          css={{
            gridArea: "1 / 2",
            justifySelf: "start",
            alignSelf: "center",
            position: "relative",
            margin: `2px 0 2px ${theme.space(1)}`,
            fontSize: 12,
          }}
        >
          {group.ref}
          <ArrowUpRight size={12} aria-hidden css={{ marginInlineStart: 2, verticalAlign: "-1px" }} />
        </ResourceLink>
      </div>
      <div id={listId} hidden={!open} css={{ paddingInlineStart: 24 }}>
        {names.length ? (
          <div css={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
            {names.map((tool) => (
              <ToolTooltip key={tool} name={tool} description={descriptions.get(tool)} placement="left">
                <span tabIndex={0} data-testid="chat-agent-context-tool" css={toolPill(theme)}>
                  {tool}
                </span>
              </ToolTooltip>
            ))}
          </div>
        ) : (
          <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>Every tool this server reports.</Text>
        )}
      </div>
    </div>
  );
}

/** A label, with an info icon explaining what it names when there is a `hint`. */
function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  const theme = useTheme();
  return (
    <section
      css={{
        display: "grid",
        gap: theme.space(2),
        padding: `${theme.space(2.5)} ${theme.space(3)}`,
        background: theme.color.surfaceCard,
        border: `1px solid ${theme.color.border}`,
        borderRadius: theme.radius.md,
        boxShadow: "0 1px 2px rgba(0, 0, 0, 0.06)",
        minWidth: 0,
      }}
    >
      <span css={{ display: "inline-flex", alignItems: "center", gap: theme.space(1) }}>
        <Text css={{ color: theme.color.textMuted, fontSize: 11, textTransform: "uppercase", letterSpacing: 0.6 }}>
          {label}
        </Text>
        {hint ? (
          <Tooltip title={hint} trigger={["hover", "focus"]}>
            <Info
              size={12}
              role="img"
              aria-label={hint}
              tabIndex={0}
              data-testid={`chat-agent-context-hint-${label.toLowerCase().replace(/\s+/g, "-")}`}
              css={hintIcon(theme)}
            />
          </Tooltip>
        ) : null}
      </span>
      {children}
    </section>
  );
}
