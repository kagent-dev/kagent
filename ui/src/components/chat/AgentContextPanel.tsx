import { useId, useMemo, useState } from "react";
import { referenceLabel } from "@/api/domain/agentTemplates";
import { Alert, Button, Skeleton, Space, Tag, Tooltip, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { ChevronRight, Info } from "lucide-react";
import { useLocation, type To } from "react-router-dom";
import {
  bindingGroupKind,
  groupToolsByServer,
  indexToolServers,
  type ServerToolGroup,
} from "@/api/domain/templateToolCount";
import { useAgent, useAgentTemplate, useMcpServers, bareName, type AgentInstance, type ObjectReference } from "@/api";
import { ResourceLink } from "@/components/common/ResourceLink";
import { resourceIcons } from "@/components/common/resourceIcons";
import { buildPath, mcpServerTo, paths } from "@/router/routes";
import { backLinkState, type BackLinkState } from "@/router/backLink";
import { toolAnchor } from "@/components/mcp/toolAnchor";
import { ToolTooltip } from "@/components/mcp/ToolTooltip";

const { Text, Paragraph } = Typography;

/**
 * What this agent is, beside the conversation.
 *
 * ## Where it comes from, which is not the conversation
 *
 * A conversation is an `AgentInstance`, and an instance holds no configuration at all —
 * an id, a state, a harness, a template. Everything a reader wants here (what model is
 * answering, what it was told to do, what tools it can reach) lives on the
 * `AgentTemplate` the instance was cut from, so that is what this reads.
 *
 * That is a better answer than the panel it replaces rather than a smaller one. The
 * question somebody asks with a transcript in front of them — "does it even have a tool
 * for that" — is answered by the template, and the template is a thing they can open and
 * change. It is also shared: every conversation with this agent reads the same one, and
 * the link below goes to the page that says so before letting anybody edit it.
 *
 * ## What it does not claim
 *
 * The template's *current* contents, which are not necessarily what this conversation
 * was prepared against — a template can be edited after an instance is cut from it, and
 * the instance keeps running from its prepared revision. The panel says which revision
 * the conversation holds so the two can be told apart, rather than quietly implying
 * they are the same thing.
 */
export function AgentContextPanel({
  instance,
  agentRef,
}: {
  /** A conversation, when there is one open. */
  instance?: AgentInstance;
  /**
   * The agent itself, for surfaces with no conversation open.
   *
   * The panel is *about* the agent — its model, its instructions, its tools all live on
   * the template — so an agent's own page can show it without an instance to read them
   * through. Only the prepared revision needs a conversation, and it is omitted when
   * there is none rather than guessed at.
   */
  agentRef?: { namespace: string; name?: string };
}) {
  const theme = useTheme();
  const location = useLocation();

  const namespace = instance?.agent?.split("/")[0] ?? agentRef?.namespace ?? "";
  const name = instance?.agent ? bareName(instance.agent) : agentRef?.name;
  const definition = useAgent(namespace, name);
  const templateName = definition.data?.resource.spec.template.ref?.name;
  const template = useAgentTemplate(namespace, templateName);
  const templateNamespace = namespace;
  const templateRef = templateName;
  const harnessRef = definition.data?.resource.spec.harnessRef.name;
  const spec = definition.data?.resource.spec.template.inline ?? template.data?.resource.spec;
  const error = definition.error ?? template.error;
  const tools = spec?.tools ?? [];
  const subAgents = tools.flatMap((binding) => (binding.subAgent ? [binding.subAgent] : []));
  const mcpServers = useMcpServers();
  const serversByRef = useMemo(() => indexToolServers(mcpServers.data ?? []), [mcpServers.data]);
  // Every link here leads back to this panel's page, so its target can return.
  const back = backLinkState(location, instance ? "Back to conversation" : "Back to agent");
  const linkCss = { justifySelf: "start", fontSize: 12 } as const;
  const refPath = (path: string, ref: ObjectReference) =>
    buildPath(path, { namespace: ref.namespace || templateNamespace, name: ref.name });
  const serverTo = (ref: ObjectReference, hash?: string) =>
    mcpServerTo({ namespace: ref.namespace || templateNamespace, name: ref.name, groupKind: bindingGroupKind(ref) }, hash);

  return (
    <div data-testid="chat-agent-context" css={{ display: "grid", gap: theme.space(4) }}>
      <Field
        label="Agent template"
        hint={
          definition.data?.resource.spec.template.inline
            ? "This agent has no AgentTemplate: the model, tools and instructions below are written into the agent itself."
            : "The AgentTemplate that sets the model, tools and instructions below. Every agent built from it shares them."
        }
      >
        {templateRef && templateNamespace && templateName ? (
          <ResourceLink
            mono
            css={linkCss}
            to={buildPath(paths.agentTemplateDetail, {
              namespace: templateNamespace,
              name: templateName,
            })}
            state={back}
            icon={resourceIcons.template}
            tooltip="Open the AgentTemplate this agent is built from"
            data-testid="chat-agent-context-template"
          >
            {`${templateNamespace}/${templateRef}`}
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
        hint="The runtime that executes this agent, such as the built-in kagent engine, Codex or Claude."
      >
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

      <div
        role="separator"
        css={{ borderTop: `1px solid ${theme.color.border}`, margin: `${theme.space(1)} 0` }}
      />

      {definition.isLoading || template.isLoading ? (
        <Skeleton active paragraph={{ rows: 4 }} data-testid="chat-agent-context-loading" />
      ) : error ? (
        /* The conversation above is unaffected — it is read from the gateway, not from
           the template — so this is a note beside the transcript rather than a failure
           of the page. */
        <Alert
          type="warning"
          showIcon
          data-testid="chat-agent-context-error"
          title="Could not read this agent's template"
          description={error.message}
        />
      ) : spec ? (
        <>
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
              // Said rather than left blank: an agent with no tools is a real and
              // ordinary configuration, and an empty panel reads as a failed read.
              <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
                None — this agent answers from the model alone.
              </Text>
            ) : (
              <div css={{ display: "grid", gap: theme.space(2) }}>
                {groupToolsByServer(tools, serversByRef, templateNamespace).map((group) => (
                  <ServerTools
                    key={`${group.groupKind}|${group.ref}`}
                    group={group}
                    to={(hash) => serverTo(group.server, hash)}
                    back={back}
                  />
                ))}
                {subAgents.length ? (
                  <Space size={4} wrap>
                    {subAgents.map((subAgent, index) => (
                      <Tag key={`${index}-${subAgent.name}`} color="processing" css={{ marginInlineEnd: 0 }}>
                        {subAgent.name}
                      </Tag>
                    ))}
                  </Space>
                ) : null}
              </div>
            )}
          </Field>

          {spec.systemPrompt ? (
            <Field label="Instructions" hint="The system prompt the agent starts every conversation with.">
              <Paragraph
                data-testid="chat-agent-context-prompt"
                css={{
                  margin: 0,
                  fontSize: 12,
                  color: theme.color.textMuted,
                  whiteSpace: "pre-wrap",
                }}
                ellipsis={{ rows: 8, expandable: true, symbol: "more" }}
              >
                {spec.systemPrompt}
              </Paragraph>
            </Field>
          ) : spec.systemPromptFrom ? (
            <Field label="Instructions" hint="The system prompt the agent starts every conversation with.">
              {/* A prompt held in a ConfigMap is not on the template and cannot be shown
                  here. Saying where it is beats an empty box that reads as an agent with
                  no instructions. */}
              <Text css={{ fontSize: 12, color: theme.color.textMuted }}>
                Read at runtime from the {spec.systemPromptFrom.name} ConfigMap, key{" "}
                {spec.systemPromptFrom.key}.
              </Text>
            </Field>
          ) : null}
        </>
      ) : null}

      {instance?.preparedRevision ? (
        <Field
          label="Prepared revision"
          hint="The configuration this conversation was prepared with. Later edits to the agent or its template don't apply to it."
        >
          {/* What this conversation actually runs. The template above can be edited
              after an instance is cut from it, and the instance keeps its revision —
              so the two are shown side by side rather than one standing for both.
              A full digest is noise at a glance, so it is shortened, with the whole
              value one copy away. */}
          <Text
            data-testid="chat-agent-context-revision"
            copyable={{ text: instance.preparedRevision, tooltips: ["Copy full revision", "Copied"] }}
            css={{ fontFamily: theme.font.mono, fontSize: 11, color: theme.color.textMuted }}
          >
            {instance.preparedRevision.slice(0, 12)}
          </Text>
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

/** One MCP server's row: a link to it, a tool count, and its tools behind a toggle. */
function ServerTools({ group, to, back }: { group: ServerToolGroup; to: (hash?: string) => To; back: BackLinkState }) {
  const theme = useTheme();
  const [open, setOpen] = useState(false);
  const listId = useId();
  const descriptions = new Map(group.discovered?.map((tool) => [tool.name, tool.description]));
  const names = group.allTools ? (group.discovered ?? []).map((tool) => tool.name) : group.selected;

  return (
    <div data-testid="chat-agent-context-server-group" css={{ display: "grid", gap: theme.space(1) }}>
      <div css={{ display: "flex", alignItems: "flex-start", gap: theme.space(1), minWidth: 0 }}>
        <Button
          type="text"
          size="small"
          aria-expanded={open}
          aria-controls={listId}
          aria-label={`${open ? "Hide" : "Show"} tools from ${group.ref}`}
          data-testid="chat-agent-context-server-toggle"
          onClick={() => setOpen((value) => !value)}
          icon={
            <ChevronRight
              size={14}
              aria-hidden
              css={{ transition: "transform 120ms ease", transform: open ? "rotate(90deg)" : undefined }}
            />
          }
          css={{ flexShrink: 0, width: 20, minWidth: 20, height: 20, padding: 0, color: theme.color.textMuted }}
        />
        <ResourceLink
          mono
          icon={resourceIcons.mcpServer}
          to={to()}
          state={back}
          data-testid="chat-agent-context-server"
          css={{ fontSize: 12, flex: "0 1 auto", lineHeight: "20px" }}
        >
          {group.ref}
        </ResourceLink>
        <Tag data-testid="chat-agent-context-server-count" css={{ marginInlineEnd: 0, marginInlineStart: "auto", flexShrink: 0, fontSize: 11 }}>
          {toolCountLabel(group)}
        </Tag>
      </div>
      <div id={listId} hidden={!open} css={{ paddingInlineStart: 24 }}>
        {names.length ? (
          <Space
            size={4}
            wrap
            // A long tool name would otherwise run past the panel's edge.
            css={{ maxWidth: "100%", ".ant-tag": { whiteSpace: "normal", wordBreak: "break-word", marginInlineEnd: 0 } }}
          >
            {names.map((tool) => (
              <Tag key={tool} css={{ fontFamily: theme.font.mono }}>
                <ToolTooltip name={tool} description={descriptions.get(tool)}>
                  <ResourceLink to={to(toolAnchor(tool))} state={back} data-testid="chat-agent-context-tool">
                    {tool}
                  </ResourceLink>
                </ToolTooltip>
              </Tag>
            ))}
          </Space>
        ) : (
          <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>Every tool this server reports.</Text>
        )}
      </div>
    </div>
  );
}

/** A label, with an info icon explaining what it names when there is a `hint`. */
function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  const theme = useTheme();
  return (
    <div css={{ display: "grid", gap: theme.space(1) }}>
      <span css={{ display: "inline-flex", alignItems: "center", gap: theme.space(1) }}>
        <Text
          css={{
            color: theme.color.textMuted,
            fontSize: 11,
            textTransform: "uppercase",
            letterSpacing: 0.6,
          }}
        >
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
              css={{ color: theme.color.textMuted, cursor: "help", flexShrink: 0 }}
            />
          </Tooltip>
        ) : null}
      </span>
      {children}
    </div>
  );
}
