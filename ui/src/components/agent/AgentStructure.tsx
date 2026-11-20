import { createContext, useContext, useId, useState, type ReactNode } from "react";
import { Alert, Button, Typography } from "antd";
import { useTheme, type Theme } from "@emotion/react";
import { ChevronRight } from "lucide-react";
import { Link, useLocation } from "react-router-dom";
import {
  referenceLabel,
  useAgentTemplate,
  useAgentsAcrossNamespaces,
  useHarnesses,
  type Agent,
  type AgentTemplateSpec,
  type McpToolBinding,
  type SubAgentToolBinding,
} from "@/api";
import { groupMcpTools } from "./groupMcpTools";
import { ToolServerGroup } from "./ToolServerGroup";
import { linkInk } from "@/components/common/linkStyles";
import { buildPath, paths } from "@/router/routes";
import { backLinkState } from "@/router/backLink";

const { Text } = Typography;

/** Sub-agents expand in place this many levels deep; deeper ones link to their template. */
const MAX_DEPTH = 2;
/** Prompts longer than this start clamped, with a "Show all" toggle. */
const PROMPT_CLAMP_LINES = 3;
const PROMPT_CLAMP_CHARS = 240;

/** How deeply a node is nested, so neighbouring levels alternate surfaces. */
const NestLevel = createContext(0);

const RUNTIME_LABEL: Record<string, string> = {
  kagent: "kagent runtime",
  codex: "Codex runtime",
  claude: "Claude runtime",
  byo: "Custom image runtime",
};

/**
 * What an agent is made of, as a tree: the Harness that runs it, the template that
 * defines it, and the template's model, instructions, tools and sub-agents.
 *
 * `compact` drops the per-node descriptions and tightens spacing for narrow panels.
 */
export function AgentStructure({ agent, compact = false }: { agent: Agent; compact?: boolean }) {
  const theme = useTheme();
  const location = useLocation();
  const { namespace } = agent;
  const { spec } = agent.resource;
  const templateName = spec.template.ref?.name;
  const inline = spec.template.inline;

  const template = useAgentTemplate(namespace, templateName);
  const harnesses = useHarnesses(namespace);
  const sameNamespaceAgents = useAgentsAcrossNamespaces(templateName ? [namespace] : undefined);

  const runtime = harnesses.data?.find((harness) => harness.name === spec.harnessRef.name)?.runtime;
  const sharedBy = templateName
    ? (sameNamespaceAgents.data?.agents ?? []).filter(
        (other) => other.resource.spec.template.ref?.name === templateName,
      ).length
    : 0;
  const templateSpec = inline ?? template.data?.resource.spec;

  return (
    <section
      aria-label="Agent structure"
      data-testid="agent-structure"
      data-compact={compact || undefined}
      css={{ display: "grid", minWidth: 0 }}
    >
      <StructureNode
        compact={compact}
        testId="structure-harness"
        kind="Harness"
        name={<Mono>{`${namespace}/${spec.harnessRef.name}`}</Mono>}
        description="Runs the agent. It owns the loop, tool calls and sandbox."
        badges={runtime ? <Pill tone="info">{RUNTIME_LABEL[runtime] ?? runtime}</Pill> : null}
      />
      <div
        aria-hidden
        css={{
          width: 2,
          height: compact ? theme.space(2) : theme.space(4),
          marginInlineStart: compact ? theme.space(5) : theme.space(7),
          background: theme.color.border,
        }}
      />
      <StructureNode
        compact={compact}
        testId="structure-template"
        kind="Agent template"
        name={
          templateName ? (
            <Link
              to={buildPath(paths.agentTemplateDetail, { namespace, name: templateName })}
              state={backLinkState(location, "Back to agent")}
              data-testid="agent-template-link"
              css={[monoCss(theme), linkInk(theme), focusRing(theme)]}
            >
              {`${namespace}/${templateName}`}
            </Link>
          ) : (
            <Text>Written into this agent</Text>
          )
        }
        description={
          templateName
            ? "Defines what the agent does. Agents built from it share these settings."
            : "Defines what the agent does. Only this agent uses it."
        }
        badges={
          inline ? (
            <Pill tone="accent" testId="agent-template-inline">Inline</Pill>
          ) : sharedBy > 1 ? (
            <Pill tone="info" testId="agent-template-shared">{`Shared by ${sharedBy} agents`}</Pill>
          ) : null
        }
      >
        <ChildList compact={compact} indent>
          {template.error ? (
            <Alert
              type="warning"
              showIcon
              data-testid="structure-template-error"
              title="Could not read this agent's template"
              description={template.error.message}
            />
          ) : templateSpec ? (
            <TemplateParts spec={templateSpec} namespace={namespace} depth={0} compact={compact} />
          ) : (
            <TemplatePartsSkeleton compact={compact} />
          )}
        </ChildList>
      </StructureNode>
    </section>
  );
}

/** The model, instructions, tools and sub-agents of one template, as child nodes. */
function TemplateParts({
  spec,
  namespace,
  depth,
  compact,
}: {
  spec: AgentTemplateSpec;
  namespace: string;
  depth: number;
  compact: boolean;
}) {
  const theme = useTheme();
  const bindings = spec.tools ?? [];
  const servers = bindings.flatMap((binding) => (binding.mcp ? [binding.mcp] : []));
  const subAgents = bindings.flatMap((binding) => (binding.subAgent ? [binding.subAgent] : []));
  const model = referenceLabel(spec.modelConfig, namespace);

  return (
    <>
      <StructureNode
        compact={compact}
        child
        testId="structure-model"
        kind="Model"
        name={model ? <Mono>{model}</Mono> : <Muted>None</Muted>}
        description="The language model that writes the answers."
      />

      {spec.systemPrompt ? (
        <StructureNode
          compact={compact}
          child
          testId="structure-instructions"
          kind="Instructions"
          name="System prompt"
          description="What the agent is told before every conversation."
        >
          <Prompt text={spec.systemPrompt} />
        </StructureNode>
      ) : (
        <StructureNode
          compact={compact}
          child
          testId="structure-instructions"
          kind="Instructions"
          name={
            spec.systemPromptFrom ? (
              <span>
                ConfigMap <Mono>{spec.systemPromptFrom.name}</Mono>, key{" "}
                <Mono>{spec.systemPromptFrom.key}</Mono>
              </span>
            ) : (
              <Muted>None</Muted>
            )
          }
          description={
            spec.systemPromptFrom
              ? "Read when the agent starts, so it is not shown here."
              : "What the agent is told before every conversation."
          }
        />
      )}

      {servers.length > 0 ? (
        <StructureNode
          compact={compact}
          child
          testId="structure-tools"
          kind="Tools"
          name={toolSummary(servers, namespace)}
          description="Actions the agent can take through MCP servers."
        >
          <div css={{ display: "grid", gap: theme.space(3) }}>
            {groupMcpTools(servers, namespace).map((group) => (
              <ToolServerGroup key={group.server} group={group} />
            ))}
          </div>
        </StructureNode>
      ) : (
        <StructureNode
          compact={compact}
          child
          testId="structure-tools"
          kind="Tools"
          name={<Muted>None</Muted>}
          description="This agent answers from the model alone."
        />
      )}

      {subAgents.length > 0 ? (
        <StructureNode
          compact={compact}
          child
          testId="structure-subagents"
          kind="Sub-agents"
          name={`${subAgents.length} ${subAgents.length === 1 ? "sub-agent" : "sub-agents"}`}
          description="Other agents this one can hand work to."
          defaultOpen={depth === 0}
        >
          <ChildList compact={compact}>
            {subAgents.map((subAgent) =>
              depth >= MAX_DEPTH ? (
                <SubAgentLink
                  key={subAgent.name}
                  subAgent={subAgent}
                  namespace={namespace}
                />
              ) : (
                <SubAgentNode
                  key={subAgent.name}
                  subAgent={subAgent}
                  namespace={namespace}
                  depth={depth + 1}
                  compact={compact}
                />
              ),
            )}
          </ChildList>
        </StructureNode>
      ) : null}
    </>
  );
}

function toolSummary(servers: readonly McpToolBinding[], namespace: string): string {
  const groups = groupMcpTools(servers, namespace);
  const serverText = `${groups.length} MCP ${groups.length === 1 ? "server" : "servers"}`;
  // A server with no tool list exposes everything it has, which is not countable here.
  if (groups.some((group) => group.rows.some((row) => row.kind !== "tool"))) return serverText;
  const tools = groups.reduce((total, group) => total + group.namedCount, 0);
  return `${serverText}, ${tools} ${tools === 1 ? "tool" : "tools"}`;
}

/** A sub-agent that expands in place, reading its template only once opened. */
function SubAgentNode({
  subAgent,
  namespace,
  depth,
  compact,
}: {
  subAgent: SubAgentToolBinding;
  namespace: string;
  depth: number;
  compact: boolean;
}) {
  return (
    <StructureNode
      compact={compact}
      child
      testId={`structure-subagent-${subAgent.name}`}
      kind="Sub-agent"
      name={subAgent.name}
      description={subAgent.description}
      badges={<Pill tone="info">{`Template ${subAgent.templateRef.name}`}</Pill>}
    >
      <SubAgentBody subAgent={subAgent} namespace={namespace} depth={depth} compact={compact} />
    </StructureNode>
  );
}

function SubAgentBody({
  subAgent,
  namespace,
  depth,
  compact,
}: {
  subAgent: SubAgentToolBinding;
  namespace: string;
  depth: number;
  compact: boolean;
}) {
  const template = useAgentTemplate(namespace, subAgent.templateRef.name);
  const spec = template.data?.resource.spec;

  return (
    <ChildList compact={compact}>
      <TemplateLink namespace={namespace} name={subAgent.templateRef.name} />
      {template.error ? (
        <Alert
          type="warning"
          showIcon
          data-testid="structure-subagent-error"
          title="Could not read this sub-agent's template"
          description={template.error.message}
        />
      ) : spec ? (
        <TemplateParts spec={spec} namespace={namespace} depth={depth} compact={compact} />
      ) : (
        <TemplatePartsSkeleton compact={compact} />
      )}
    </ChildList>
  );
}

/** Past `MAX_DEPTH` a sub-agent is a link to its template rather than another level. */
function SubAgentLink({ subAgent, namespace }: { subAgent: SubAgentToolBinding; namespace: string }) {
  const theme = useTheme();
  return (
    <div data-testid={`structure-subagent-link-${subAgent.name}`} css={{ display: "grid", gap: theme.space(0.5) }}>
      <Text css={{ fontWeight: 600 }}>{subAgent.name}</Text>
      <Text css={{ color: theme.color.textMuted, fontSize: 13 }}>{subAgent.description}</Text>
      <TemplateLink namespace={namespace} name={subAgent.templateRef.name} />
    </div>
  );
}

function TemplateLink({ namespace, name }: { namespace: string; name: string }) {
  const theme = useTheme();
  const location = useLocation();
  return (
    <Link
      to={buildPath(paths.agentTemplateDetail, { namespace, name })}
      state={backLinkState(location, "Back to agent")}
      css={[{ fontSize: 13, justifySelf: "start" }, linkInk(theme), focusRing(theme)]}
    >
      Open template <span css={monoCss(theme)}>{`${namespace}/${name}`}</span>
    </Link>
  );
}

/** A system prompt, clamped to a few lines when long. */
function Prompt({ text }: { text: string }) {
  const theme = useTheme();
  const [expanded, setExpanded] = useState(false);
  const long = text.split("\n").length > PROMPT_CLAMP_LINES || text.length > PROMPT_CLAMP_CHARS;
  const clamped = long && !expanded;

  return (
    <div css={{ display: "grid", gap: theme.space(2), minWidth: 0 }}>
      <p
        data-testid="structure-prompt"
        css={{
          margin: 0,
          whiteSpace: "pre-wrap",
          wordBreak: "break-word",
          color: theme.color.textMuted,
          fontSize: 13,
          ...(clamped
            ? {
                display: "-webkit-box",
                WebkitLineClamp: PROMPT_CLAMP_LINES,
                WebkitBoxOrient: "vertical",
                overflow: "hidden",
              }
            : {}),
        }}
      >
        {text}
      </p>
      {long ? (
        <Button
          size="small"
          css={{ justifySelf: "start" }}
          aria-expanded={expanded}
          data-testid="structure-prompt-toggle"
          onClick={() => setExpanded((open) => !open)}
        >
          {expanded ? "Show less" : "Show all"}
        </Button>
      ) : null}
    </div>
  );
}

/** Placeholders the size of the three child nodes every template has, so the tree does not jump when the template lands. */
function TemplatePartsSkeleton({ compact }: { compact: boolean }) {
  const kinds: [string, string][] = [
    ["Model", "The language model that writes the answers."],
    ["Instructions", "What the agent is told before every conversation."],
    ["Tools", "Actions the agent can take through MCP servers."],
  ];
  return (
    <div data-testid="structure-loading" css={{ display: "contents" }}>
      {kinds.map(([kind, description]) => (
        <StructureNode
          key={kind}
          compact={compact}
          child
          kind={kind}
          name={<NamePlaceholder />}
          description={description}
        />
      ))}
    </div>
  );
}

/** A bar one text line tall, standing in for a node's name. */
function NamePlaceholder() {
  const theme = useTheme();
  return (
    <span css={{ display: "flex", alignItems: "center", height: "1.5em" }}>
      <span
        css={{
          width: 180,
          maxWidth: "100%",
          height: 12,
          borderRadius: theme.radius.sm,
          background: theme.color.border,
        }}
      />
    </span>
  );
}

/** The skeleton for the whole tree, before the Agent itself is read. */
export function AgentStructureSkeleton({ compact = false }: { compact?: boolean }) {
  const theme = useTheme();
  const placeholder = <NamePlaceholder />;
  return (
    <div data-testid="agent-structure-loading" css={{ display: "grid", minWidth: 0 }}>
      <StructureNode
        compact={compact}
        kind="Harness"
        name={placeholder}
        description="Runs the agent. It owns the loop, tool calls and sandbox."
      />
      <div
        aria-hidden
        css={{
          width: 2,
          height: compact ? theme.space(2) : theme.space(4),
          marginInlineStart: compact ? theme.space(5) : theme.space(7),
          background: theme.color.border,
        }}
      />
      <StructureNode
        compact={compact}
        kind="Agent template"
        name={placeholder}
        description="Defines what the agent does. Agents built from it share these settings."
      >
        <ChildList compact={compact} indent>
          <TemplatePartsSkeleton compact={compact} />
        </ChildList>
      </StructureNode>
    </div>
  );
}

/**
 * One box in the tree. A `child` node with `children` has a button head that shows and
 * hides them, and draws connectors to its parent.
 */
function StructureNode({
  kind,
  name,
  description,
  badges,
  children,
  defaultOpen = false,
  child = false,
  compact,
  testId,
}: {
  kind: string;
  name: ReactNode;
  description: string;
  badges?: ReactNode;
  children?: ReactNode;
  defaultOpen?: boolean;
  child?: boolean;
  compact: boolean;
  testId?: string;
}) {
  const theme = useTheme();
  const level = useContext(NestLevel);
  const bodyId = useId();
  const [open, setOpen] = useState(defaultOpen);
  // A top-level node shows its children always (the template's parts); a child node toggles them.
  const expandable = child && children !== undefined;
  const tick = compact ? TICK_COMPACT : TICK;
  const tickTop = compact ? 16 : 24;
  const padding = compact ? `${theme.space(2)} ${theme.space(3)}` : `${theme.space(3.5)} ${theme.space(4)}`;

  const head = (
    <>
      <span
        aria-hidden
        css={{
          width: 16,
          display: "inline-flex",
          paddingTop: 2,
          color: theme.color.textMuted,
          flexShrink: 0,
        }}
      >
        {expandable ? (
          <ChevronRight
            size={16}
            css={{
              transition: "transform 120ms",
              transform: open ? "rotate(90deg)" : undefined,
              "@media (prefers-reduced-motion: reduce)": { transition: "none" },
            }}
          />
        ) : null}
      </span>
      <span css={{ flex: "1 1 200px", minWidth: 0, display: "grid", gap: 2 }}>
        <span
          css={{
            fontSize: 11,
            letterSpacing: 0.8,
            textTransform: "uppercase",
            fontWeight: 600,
            color: theme.color.textMuted,
          }}
        >
          {kind}
        </span>
        <span css={{ fontWeight: 600, wordBreak: "break-word", color: theme.color.text }}>{name}</span>
        {compact ? null : (
          <span css={{ color: theme.color.textMuted, fontSize: 13 }}>{description}</span>
        )}
      </span>
      {badges ? (
        <span
          css={{
            display: "flex",
            flexWrap: "wrap",
            gap: theme.space(1.5),
            alignItems: "flex-start",
            marginInlineStart: "auto",
          }}
        >
          {badges}
        </span>
      ) : null}
    </>
  );

  const headCss = {
    display: "flex",
    flexWrap: "wrap" as const,
    alignItems: "flex-start",
    columnGap: theme.space(3),
    rowGap: theme.space(2),
    width: "100%",
    padding,
    textAlign: "left" as const,
    font: "inherit",
    color: "inherit",
    background: "none",
    border: 0,
    borderRadius: theme.radius.md,
  };

  return (
    <div
      data-testid={testId}
      data-open={expandable ? open : undefined}
      css={{
        position: "relative",
        minWidth: 0,
        border: `1px solid ${theme.color.border}`,
        borderRadius: theme.radius.md,
        background: level % 2 === 0 ? theme.color.bgElevated : theme.color.bg,
        // Connectors: a tick into each child, and a rail down to the next one that stops at the last.
        ...(child
          ? {
              "&::before, &::after": {
                content: '""',
                position: "absolute",
                background: theme.color.border,
              },
              "&::before": {
                insetInlineStart: -tick - 1,
                top: tickTop,
                width: tick,
                height: 2,
              },
              "&::after": {
                insetInlineStart: -tick - 1,
                top: `-${theme.space(2)}`,
                bottom: -1,
                width: 2,
              },
              "&:last-child::after": { bottom: "auto", height: `calc(${theme.space(2)} + ${tickTop + 2}px)` },
            }
          : {}),
      }}
    >
      {expandable ? (
        <button
          type="button"
          aria-expanded={open}
          aria-controls={bodyId}
          data-testid={testId ? `${testId}-toggle` : undefined}
          onClick={() => setOpen((value) => !value)}
          css={[
            headCss,
            {
              cursor: "pointer",
              "&:hover": { background: `${theme.color.text}14` },
              "&:active": { background: `${theme.color.primary}33` },
            },
            focusRing(theme),
          ]}
        >
          {head}
        </button>
      ) : (
        <div css={headCss}>{head}</div>
      )}
      {children !== undefined && (!expandable || open) ? (
        <div
          id={bodyId}
          data-testid={testId ? `${testId}-body` : undefined}
          css={
            expandable
              ? {
                  padding: compact
                    ? `0 ${theme.space(3)} ${theme.space(3)} ${theme.space(3)}`
                    : `0 ${theme.space(4)} ${theme.space(3.5)} ${theme.space(11)}`,
                  "@media (max-width: 720px)": { paddingInlineStart: theme.space(4) },
                }
              : undefined
          }
        >
          <NestLevel.Provider value={level + 1}>{children}</NestLevel.Provider>
        </div>
      ) : null}
    </div>
  );
}

/** Room on the left for the connectors each child node draws. */
function ChildList({
  children,
  compact,
  indent = false,
}: {
  children: ReactNode;
  compact: boolean;
  indent?: boolean;
}) {
  const theme = useTheme();
  const tick = compact ? TICK_COMPACT : TICK;
  return (
    <div
      css={{
        display: "grid",
        gap: theme.space(2),
        minWidth: 0,
        margin: indent ? `0 ${theme.space(compact ? 2 : 4)} ${theme.space(compact ? 2 : 4)} ${theme.space(compact ? 3 : 5)}` : 0,
        paddingInlineStart: tick,
      }}
    >
      {children}
    </div>
  );
}

const TICK = 14;
const TICK_COMPACT = 10;

type Tone = "info" | "warning" | "accent";

function Pill({ tone, testId, children }: { tone: Tone; testId?: string; children: ReactNode }) {
  const theme = useTheme();
  const colors = {
    info: [theme.color.infoBg, theme.color.infoBorder, theme.color.infoText],
    warning: [theme.color.warningBg, theme.color.warningBorder, theme.color.warningText],
    accent: [theme.color.accentBg, theme.color.accentBorder, theme.color.accentText],
  }[tone];
  return (
    <span
      data-testid={testId}
      css={{
        display: "inline-block",
        borderRadius: 999,
        padding: `0 ${theme.space(2.5)}`,
        fontSize: 12,
        lineHeight: "20px",
        fontWeight: 600,
        whiteSpace: "nowrap",
        background: colors[0],
        border: `1px solid ${colors[1]}`,
        color: colors[2],
      }}
    >
      {children}
    </span>
  );
}

function Mono({ children }: { children: ReactNode }) {
  const theme = useTheme();
  return <span css={monoCss(theme)}>{children}</span>;
}

function Muted({ children }: { children: ReactNode }) {
  const theme = useTheme();
  return <span css={{ color: theme.color.textMuted, fontWeight: 400 }}>{children}</span>;
}

function monoCss(theme: Theme) {
  return { fontFamily: theme.font.mono, fontSize: 12.5 };
}

function focusRing(theme: Theme) {
  return {
    "&:focus-visible": {
      outline: `2px solid ${theme.color.primaryText}`,
      outlineOffset: -2,
    },
  };
}
