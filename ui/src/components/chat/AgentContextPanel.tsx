import { referenceLabel } from "@/api/domain/agentTemplates";
import { Alert, Skeleton, Space, Tag, Tooltip, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Info } from "lucide-react";
import { Link, useLocation } from "react-router-dom";
import { useAgent, useAgentTemplate, bareName, type AgentInstance } from "@/api";
import { buildPath, paths } from "@/router/routes";
import { backLinkState } from "@/router/backLink";

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

  return (
    <div data-testid="chat-agent-context" css={{ display: "grid", gap: theme.space(4) }}>
      {/* Only beside a conversation: on the agent's own page this would link to itself. */}
      {instance && namespace && name ? (
        <Field
          label="Agent"
          hint="The Agent resource this conversation talks to. It pairs a model, tools and instructions with a harness."
        >
          <RefLink
            to={buildPath(paths.agent, { namespace, name })}
            tooltip="Open this agent and its other conversations"
            testId="chat-agent-context-agent"
          >
            {`${namespace}/${name}`}
          </RefLink>
        </Field>
      ) : null}

      <Field
        label="Agent template"
        hint={
          definition.data?.resource.spec.template.inline
            ? "This agent has no AgentTemplate: the model, tools and instructions below are written into the agent itself."
            : "The AgentTemplate that sets the model, tools and instructions below. Every agent built from it shares them."
        }
      >
        {templateRef && templateNamespace && templateName ? (
          <RefLink
            to={buildPath(paths.agentTemplateDetail, {
              namespace: templateNamespace,
              name: templateName,
            })}
            // So the template page can lead back here rather than only to the
            // template list. Without a conversation open, "here" is the agent's page.
            state={backLinkState(location, instance ? "Back to conversation" : "Back to agent")}
            tooltip="Open the AgentTemplate this agent is built from"
            testId="chat-agent-context-template"
          >
            {`${templateNamespace}/${templateRef}`}
          </RefLink>
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
        <Text data-testid="chat-agent-context-harness" css={{ fontFamily: theme.font.mono, fontSize: 12 }}>
          {harnessRef ? `${namespace}/${harnessRef}` : "Not reported"}
        </Text>
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
            <Text css={{ fontFamily: theme.font.mono, fontSize: 12 }}>
              {referenceLabel(spec.modelConfig, templateNamespace) || "—"}
            </Text>
          </Field>

          <Field label="Tools" hint="The MCP tools and sub-agents the agent can call.">
            {tools.length === 0 ? (
              // Said rather than left blank: an agent with no tools is a real and
              // ordinary configuration, and an empty panel reads as a failed read.
              <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
                None — this agent answers from the model alone.
              </Text>
            ) : (
              <Space
                size={4}
                wrap
                // A long server and tool name would otherwise run past the panel's edge.
                css={{ maxWidth: "100%", ".ant-tag": { whiteSpace: "normal", wordBreak: "break-word", marginInlineEnd: 0 } }}
              >
                {tools.flatMap((binding, index) => {
                  const mcp = binding.mcp;
                  return mcp
                    ? mcp.tools?.length
                      ? mcp.tools.map((tool) => (
                          <Tag key={`${index}-${tool}`} css={{ fontFamily: theme.font.mono }}>
                            {referenceLabel(mcp.server, templateNamespace)}: {tool}
                          </Tag>
                        ))
                      : [
                          <Tag key={`${index}-mcp-all`} css={{ fontFamily: theme.font.mono }}>
                            {referenceLabel(mcp.server, templateNamespace)} (all tools)
                          </Tag>,
                        ]
                    : binding.subAgent
                      ? [
                          <Tag key={`${index}-agent`} color="processing">
                            {binding.subAgent.name}
                          </Tag>,
                        ]
                      : [];
                })}
              </Space>
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

/** A reference to another resource, styled as a link, with a tooltip saying where it goes. */
function RefLink({
  to,
  state,
  tooltip,
  testId,
  children,
}: {
  to: string;
  state?: unknown;
  tooltip: string;
  testId: string;
  children: React.ReactNode;
}) {
  const theme = useTheme();
  return (
    <Tooltip title={tooltip} trigger={["hover", "focus"]}>
      <Link
        to={to}
        state={state}
        data-testid={testId}
        css={{
          justifySelf: "start",
          fontFamily: theme.font.mono,
          fontSize: 12,
          color: theme.color.primaryText,
          wordBreak: "break-word",
          "&:hover, &:focus-visible": { textDecoration: "underline" },
        }}
      >
        {children}
      </Link>
    </Tooltip>
  );
}
