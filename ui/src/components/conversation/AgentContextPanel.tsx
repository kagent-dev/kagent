import { useId, useMemo, useState } from "react";
import { referenceLabel } from "@/api/domain/agentTemplates";
import { Alert, Button, Skeleton, Tooltip, Typography } from "antd";
import { useTheme, type Theme } from "@emotion/react";
import { ChevronRight, ChevronsDownUp, ChevronsUpDown, FileText, Info } from "lucide-react";
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
import type { SubAgentToolBinding } from "@/api/domain/agentTemplates";
import { ResourceLink } from "@/components/common/ResourceLink";
import { ClampedText, ToolList } from "@/components/mcp/ToolList";
import { iconControlStyles } from "@/components/agent/controlStyles";
import { resourceIcons } from "@/components/common/resourceIcons";
import { buildPath, mcpServerTo, paths } from "@/router/routes";
import { backLinkState, type BackLinkState } from "@/router/backLink";

const { Text, Paragraph } = Typography;

// region Styles

// Model, three rows of tools and a clamped prompt: the tallest of the live agents measured.
const DETAILS_MIN_HEIGHT = 328;

/** The list under a server or of sub-agents: unstyled, one row per entry. */
const toolList = (theme: Theme) =>
  ({ listStyle: "none", margin: 0, padding: 0, display: "grid", gap: theme.space(2) }) as const;

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

/** What this agent is, beside the conversation: one card per section. */
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
  /** Opens the conversation details modal. */
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
  const serverGroups = groupToolsByServer(tools, serversByRef, templateNamespace);
  // Every server starts collapsed; the card's "Expand all" opens them together.
  const [openServers, setOpenServers] = useState<ReadonlySet<string>>(() => new Set());
  const allServersOpen = serverGroups.length > 0 && serverGroups.every((group) => openServers.has(serverGroupKey(group)));
  const back = backLinkState(location, instance ? "Conversation" : "Agent");
  const linkCss = { justifySelf: "start", fontSize: 12 } as const;
  const agentTo = agentPageUrl({ namespace, name });
  const refPath = (path: string, ref: ObjectReference) =>
    buildPath(path, { namespace: ref.namespace || templateNamespace, name: ref.name });
  const serverTo = (ref: ObjectReference) =>
    mcpServerTo({ namespace: ref.namespace || templateNamespace, name: ref.name, groupKind: bindingGroupKind(ref) });

  return (
    <div
      data-testid="conversation-agent-context"
      css={{
        display: "grid",
        gap: theme.space(2),
        alignContent: "start",
        paddingInlineEnd: theme.space(1),
        paddingBlockEnd: theme.space(4),
      }}
    >
      <Field
        label="Agent"
        hint={
          instance
            ? "The Agent this conversation is with: its AgentTemplate says what it does, its Harness how it runs."
            : "This Agent: its AgentTemplate says what it does, its Harness how it runs."
        }
      >
        {agentTo ? (
          <ResourceLink
            mono
            css={linkCss}
            to={agentTo}
            state={back}
            icon={resourceIcons.agent}
            tooltip="Open this agent"
            data-testid="conversation-agent-context-agent"
          >
            {`${namespace}/${name}`}
          </ResourceLink>
        ) : (
          <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>Not reported</Text>
        )}
      </Field>

      <Field
        label="Template"
        hint={
          definition.data?.resource.spec.template.inline
            ? "This agent has no AgentTemplate: the model, tools and instructions below are written into the agent itself."
            : "The AgentTemplate that sets the model, tools and instructions below."
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
            data-testid="conversation-agent-context-template"
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

      <Field
        label="Harness"
        hint="How this agent runs: the engine behind it (the built-in kagent engine, Codex or Claude) and the platform's runtime settings."
      >
        {harnessRef && namespace ? (
          <ResourceLink
            mono
            css={linkCss}
            to={buildPath(paths.harness, { namespace, name: harnessRef })}
            state={back}
            icon={resourceIcons.harness}
            tooltip="Open the Harness this agent runs on"
            data-testid="conversation-agent-context-harness"
          >
            {`${namespace}/${harnessRef}`}
          </ResourceLink>
        ) : (
          <Text data-testid="conversation-agent-context-harness" css={{ color: theme.color.textMuted, fontSize: 12 }}>
            Not reported
          </Text>
        )}
      </Field>

      {/* The skeleton holds a typical template's height; the loaded cards take only what they need. */}
      {loading ? (
        <Skeleton
          active
          paragraph={{ rows: 7 }}
          data-testid="conversation-agent-context-loading"
          css={{ minHeight: DETAILS_MIN_HEIGHT }}
        />
      ) : error ? (
        <Alert
          type="warning"
          showIcon
          data-testid="conversation-agent-context-error"
          title="Could not read this agent's template"
          description={error.message}
        />
      ) : spec ? (
        <div
          data-testid="conversation-agent-context-spec"
          css={{ display: "grid", gap: theme.space(2) }}
        >
          <Field label="Model" hint="The ModelConfig that determines which LLM provider and model to use.">
            {spec.modelConfig?.name ? (
              <ResourceLink
                mono
                css={linkCss}
                to={refPath(paths.modelEdit, spec.modelConfig)}
                state={back}
                icon={resourceIcons.model}
                data-testid="conversation-agent-context-model"
              >
                {referenceLabel(spec.modelConfig, templateNamespace)}
              </ResourceLink>
            ) : (
              <Text css={{ fontFamily: theme.font.mono, fontSize: 12 }}>—</Text>
            )}
          </Field>

          <Field
            label="Tools"
            hint="Tools from MCP servers and referenced agent-templates that this agent can call."
            action={
              serverGroups.length > 1 ? (
                <Button
                  type="text"
                  size="small"
                  icon={allServersOpen ? <ChevronsDownUp size={14} aria-hidden /> : <ChevronsUpDown size={14} aria-hidden />}
                  onClick={() => setOpenServers(allServersOpen ? new Set() : new Set(serverGroups.map(serverGroupKey)))}
                  data-testid="conversation-agent-context-servers-toggle-all"
                  css={{ ...iconControlStyles(theme), fontSize: 12, height: 20, paddingInline: theme.space(1), marginBlock: -2 }}
                >
                  {allServersOpen ? "Collapse all" : "Expand all"}
                </Button>
              ) : null
            }
          >
            {tools.length === 0 ? (
              <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>None — this agent answers from the model alone.</Text>
            ) : (
              <div css={{ display: "grid", gap: theme.space(2) }}>
                {serverGroups.map((group) => (
                  <ServerTools
                    key={serverGroupKey(group)}
                    group={group}
                    open={openServers.has(serverGroupKey(group))}
                    onToggle={() => setOpenServers((current) => toggled(current, serverGroupKey(group)))}
                    to={serverTo(group.server)}
                    back={back}
                  />
                ))}
                {subAgents.length ? (
                  <ul
                    data-testid="conversation-agent-context-subagents"
                    css={{
                      ...toolList(theme),
                      gap: theme.space(3),
                      ...(serverGroups.length ? { borderTop: `1px solid ${theme.color.border}`, paddingTop: theme.space(2) } : {}),
                    }}
                  >
                    {subAgents.map((subAgent, index) => (
                      <SubAgentRow
                        key={`${index}-${subAgent.name}`}
                        subAgent={subAgent}
                        templateTo={refPath(paths.agentTemplateDetail, subAgent.templateRef)}
                        templateNamespace={templateNamespace}
                        back={back}
                      />
                    ))}
                  </ul>
                ) : null}
              </div>
            )}
          </Field>

          {spec.systemPrompt ? (
            <Field label="Instructions" hint="The system prompt the agent starts every conversation with.">
              <Paragraph
                data-testid="conversation-agent-context-prompt"
                css={{ "&&": { margin: 0 }, fontSize: 12, color: theme.color.textMuted, whiteSpace: "pre-wrap" }}
                ellipsis={{ rows: 8, expandable: true, symbol: "more" }}
              >
                {spec.systemPrompt}
              </Paragraph>
            </Field>
          ) : spec.systemPromptFrom ? (
            <Field label="Instructions" hint="The system prompt the agent starts every conversation with.">
              <Text css={{ fontSize: 12, color: theme.color.textMuted }}>
                Read at runtime from the {spec.systemPromptFrom.name} ConfigMap, key {spec.systemPromptFrom.key}.
              </Text>
            </Field>
          ) : null}
        </div>
      ) : null}
      {instance ? (
        <Field label="Conversation" hint="This conversation's name, ID and prepared revision.">
          <Text
            data-testid="conversation-agent-context-session-name"
            ellipsis={{ tooltip: conversationHeading(instance, autoTitle) }}
            css={{ fontSize: 12, color: theme.color.text, minWidth: 0 }}
          >
            {conversationHeading(instance, autoTitle)}
          </Text>
          <Text
            data-testid="conversation-agent-context-session"
            copyable={{ text: instance.id, tooltips: ["Copy conversation ID", "Copied"] }}
            ellipsis={{ tooltip: instance.id }}
            css={{ fontFamily: theme.font.mono, fontSize: 11, color: theme.color.textMuted, minWidth: 0 }}
          >
            {instance.id}
          </Text>
          {instance.preparedRevision ? (
            <span css={{ display: "flex", alignItems: "baseline", gap: theme.space(2), minWidth: 0 }}>
              <Text css={{ fontSize: 11, color: theme.color.textMuted, flexShrink: 0 }}>Revision</Text>
              <Text
                data-testid="conversation-agent-context-revision"
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
              data-testid="conversation-agent-context-details"
              css={{ marginTop: theme.space(1), fontSize: 12 }}
            >
              Conversation details
            </Button>
          ) : null}
        </Field>
      ) : null}
    </div>
  );
}

const serverGroupKey = (group: ServerToolGroup) => `${group.groupKind}|${group.ref}`;

/** `set` with `key` added, or removed when it was there. */
function toggled(set: ReadonlySet<string>, key: string): ReadonlySet<string> {
  const next = new Set(set);
  if (!next.delete(key)) next.add(key);
  return next;
}

/** What `count` says for one server: how many of its tools the agent takes. */
function toolCountLabel({ allTools, selected, discovered }: ServerToolGroup): string {
  const total = discovered?.length;
  const tools = (n: number) => (n === 1 ? "tool" : "tools");
  if (allTools) return total ? `All ${total}` : "All tools";
  return total ? `${selected.length} of ${total}` : `${selected.length} ${tools(selected.length)}`;
}

/** One MCP server's tools, as a collapsible group. The header toggles; the server name in it is a link. */
function ServerTools({
  group,
  to,
  back,
  open,
  onToggle,
}: {
  group: ServerToolGroup;
  to: To;
  back: BackLinkState;
  open: boolean;
  onToggle: () => void;
}) {
  const theme = useTheme();
  const listId = useId();
  const descriptions = new Map(group.discovered?.map((tool) => [tool.name, tool.description]));
  const names = group.allTools ? (group.discovered ?? []).map((tool) => tool.name) : group.selected;
  const recessed = {
    background: theme.color.bg,
    borderTop: `1px solid ${theme.color.border}`,
    padding: `${theme.space(2)} ${theme.space(2)} ${theme.space(2)} calc(${theme.space(2)} + 36px)`,
  } as const;

  return (
    <div
      data-testid="conversation-agent-context-server-group"
      css={{ border: `1px solid ${theme.color.border}`, borderRadius: theme.radius.md, overflow: "hidden" }}
    >
      {/* The button fills the header, so anywhere in it toggles; the link sits above it, so neither nests in the other. */}
      <div
        css={{
          position: "relative",
          display: "grid",
          gridTemplateColumns: "14px minmax(0, 1fr) auto",
          alignItems: "center",
          columnGap: theme.space(1),
          minHeight: 40,
          padding: `6px ${theme.space(2)}`,
          color: theme.color.textMuted,
          fontSize: 12,
          lineHeight: "20px",
          // Raised above the card; the tools below sit recessed, at the page's level.
          background: `${theme.color.text}0a`,
          "&:hover": { background: `${theme.color.text}14` },
          "&:active": { background: `${theme.color.text}1f` },
          "&:has(> button:focus-visible)": { outline: `2px solid ${theme.color.textMuted}`, outlineOffset: -2 },
          "& > :not(button)": { position: "relative", pointerEvents: "none" },
        }}
      >
        <button
          type="button"
          aria-expanded={open}
          aria-controls={listId}
          aria-label={`${open ? "Hide" : "Show"} tools from ${group.ref}`}
          data-testid="conversation-agent-context-server-toggle"
          onClick={onToggle}
          css={{ position: "absolute", inset: 0, border: 0, padding: 0, background: "transparent", cursor: "pointer", outline: "none" }}
        />
        <ChevronRight
          size={14}
          aria-hidden
          css={{ transition: "transform 150ms ease", transform: open ? "rotate(90deg)" : undefined }}
        />
        <ResourceLink
          mono
          to={to}
          state={back}
          icon={resourceIcons.mcpServer}
          tooltip="Open this MCP server"
          data-testid="conversation-agent-context-server"
          css={{ justifySelf: "start", "&&": { pointerEvents: "auto" } }}
        >
          {group.ref}
        </ResourceLink>
        <span
          data-testid="conversation-agent-context-server-count"
          css={{ paddingInlineStart: theme.space(2), fontVariantNumeric: "tabular-nums", whiteSpace: "nowrap" }}
        >
          {toolCountLabel(group)}
        </span>
      </div>
      {/* Indented past the chevron and the server icon, so each tool lines up with the server name. */}
      {/* Grows from 0fr to 1fr, so it animates to its own height; inert and hidden once closed. */}
      <div
        id={listId}
        inert={!open}
        data-testid="conversation-agent-context-server-tools"
        css={{
          display: "grid",
          gridTemplateRows: open ? "1fr" : "0fr",
          visibility: open ? "visible" : "hidden",
          transition: `grid-template-rows 50ms ease-out, visibility 0s linear ${open ? "0s" : "50ms"}`,
          "@media (prefers-reduced-motion: reduce)": { transition: "none" },
        }}
      >
      <div css={{ minHeight: 0, overflow: "hidden" }}>
      {names.length ? (
        <ToolList
          dense
          tools={names.map((name) => ({ name, description: descriptions.get(name) }))}
          itemTestId="conversation-agent-context-tool"
          data-testid="conversation-agent-context-tool-list"
          css={recessed}
        />
      ) : (
        <div css={{ ...recessed, color: theme.color.textMuted, fontSize: 12 }}>Every tool this server reports.</div>
      )}
      </div>
      </div>
    </div>
  );
}

/**
 * An AgentTemplate bound as a tool: a link to the template, what the template is, and
 * the binding's own description, which is what the agent reads to decide when to call it.
 */
function SubAgentRow({
  subAgent,
  templateTo,
  templateNamespace,
  back,
}: {
  subAgent: SubAgentToolBinding;
  templateTo: To;
  templateNamespace: string;
  back: BackLinkState;
}) {
  const theme = useTheme();
  const template = useAgentTemplate(templateNamespace, subAgent.templateRef.name);
  const when = subAgent.description || "No description given.";
  // Often the same words as the binding's: said once.
  const about = template.data?.resource.spec.description;
  const term = { fontSize: 11, lineHeight: "16px", fontWeight: 500, color: theme.color.textMuted } as const;

  return (
    <li data-testid="conversation-agent-context-subagent" css={{ display: "grid", gap: theme.space(1), minWidth: 0, fontSize: 12 }}>
      <ResourceLink
        mono
        to={templateTo}
        state={back}
        icon={resourceIcons.template}
        tooltip="An AgentTemplate this agent can call as a tool"
        data-testid="conversation-agent-context-subagent-link"
        css={{ justifySelf: "start", lineHeight: "18px" }}
      >
        {subAgent.name}
      </ResourceLink>
      {/* Indented past the template icon, so the details sit under the name. */}
      <dl css={{ margin: 0, paddingInlineStart: 18, display: "grid", gap: theme.space(1.5), minWidth: 0 }}>
        {about && about.trim() !== when.trim() ? (
          <div data-testid="conversation-agent-context-subagent-about">
            <dt css={term}>Description</dt>
            <dd css={{ margin: 0 }}><ClampedText>{about}</ClampedText></dd>
          </div>
        ) : null}
        <div data-testid="conversation-agent-context-subagent-when">
          <dt css={term}>When it&apos;s called</dt>
          <dd css={{ margin: 0 }}><ClampedText>{when}</ClampedText></dd>
        </div>
      </dl>
    </li>
  );
}

/** A label, with an info icon explaining what it names when there is a `hint`. */
function Field({
  label,
  hint,
  action,
  children,
}: {
  label: string;
  hint?: string;
  /** A control for the whole card, at the label row's far end. */
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
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
      <span css={{ display: "flex", alignItems: "center", gap: theme.space(1), minHeight: 20 }}>
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
              data-testid={`conversation-agent-context-hint-${label.toLowerCase().replace(/\s+/g, "-")}`}
              css={hintIcon(theme)}
            />
          </Tooltip>
        ) : null}
        {action ? <span css={{ marginInlineStart: "auto" }}>{action}</span> : null}
      </span>
      {children}
    </section>
  );
}
