import { Alert, Skeleton, Space, Tag, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Link } from "react-router-dom";
import { useAgentTemplate, type AgentInstance } from "@/api";
import { buildPath, paths } from "@/router/routes";

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
  agent,
  pair,
}: {
  /** A conversation, when there is one open. */
  agent?: AgentInstance;
  /**
   * The agent itself, for surfaces with no conversation open.
   *
   * The panel is *about* the agent — its model, its instructions, its tools all live on
   * the template — so an agent's own page can show it without an instance to read them
   * through. Only the prepared revision needs a conversation, and it is omitted when
   * there is none rather than guessed at.
   */
  pair?: { namespace: string; agentTemplate?: string; harness?: string };
}) {
  const theme = useTheme();

  const namespace = agent?.namespace ?? pair?.namespace ?? "";
  const templateRef = agent?.agentTemplate ?? pair?.agentTemplate;
  const harnessRef = agent?.harness ?? pair?.harness;

  const slash = templateRef?.indexOf("/") ?? -1;
  const templateNamespace = slash === -1 ? namespace : templateRef!.slice(0, slash);
  const templateName = slash === -1 ? templateRef : templateRef!.slice(slash + 1);
  const template = useAgentTemplate(templateNamespace, templateName);

  const spec = template.data?.resource.spec;
  const tools = spec?.tools ?? [];

  return (
    <div data-testid="chat-agent-context" css={{ display: "grid", gap: theme.space(4) }}>
      <Field label="Agent">
        {templateRef && templateNamespace && templateName ? (
          <Link
            to={buildPath(paths.agentTemplateDetail, {
              namespace: templateNamespace,
              name: templateName,
            })}
            data-testid="chat-agent-context-template"
          >
            <Text css={{ fontFamily: theme.font.mono, fontSize: 12 }}>
              {templateRef}
            </Text>
          </Link>
        ) : (
          <Text css={{ color: theme.color.textMuted }}>Not reported</Text>
        )}
      </Field>

      <Field label="Runs on">
        <Text css={{ fontFamily: theme.font.mono, fontSize: 12 }}>
          {harnessRef ?? "Not reported"}
        </Text>
      </Field>

      {template.isLoading ? (
        <Skeleton active paragraph={{ rows: 4 }} data-testid="chat-agent-context-loading" />
      ) : template.error ? (
        /* The conversation above is unaffected — it is read from the gateway, not from
           the template — so this is a note beside the transcript rather than a failure
           of the page. */
        <Alert
          type="warning"
          showIcon
          data-testid="chat-agent-context-error"
          title="Could not read this agent's template"
          description={template.error.message}
        />
      ) : spec ? (
        <>
          <Field label="Model">
            <Text css={{ fontFamily: theme.font.mono, fontSize: 12 }}>
              {spec.modelConfig?.name ?? "—"}
            </Text>
          </Field>

          <Field label="Tools">
            {tools.length === 0 ? (
              // Said rather than left blank: an agent with no tools is a real and
              // ordinary configuration, and an empty panel reads as a failed read.
              <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
                None — this agent answers from the model alone.
              </Text>
            ) : (
              <Space size={4} wrap>
                {tools.flatMap((binding, index) =>
                  binding.mcp
                    ? binding.mcp.tools.map((tool) => (
                        <Tag key={`${index}-${tool}`} css={{ fontFamily: theme.font.mono }}>
                          {tool}
                        </Tag>
                      ))
                    : binding.agent
                      ? [
                          <Tag key={`${index}-agent`} color="processing">
                            {binding.agent.name}
                          </Tag>,
                        ]
                      : [],
                )}
              </Space>
            )}
          </Field>

          {spec.systemPrompt ? (
            <Field label="Instructions">
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
            <Field label="Instructions">
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

      {agent?.preparedRevision ? (
        <Field label="Prepared revision">
          {/* What this conversation actually runs. The template above can be edited
              after an instance is cut from it, and the instance keeps its revision —
              so the two are shown side by side rather than one standing for both. */}
          <Text
            css={{
              fontFamily: theme.font.mono,
              fontSize: 11,
              color: theme.color.textMuted,
              wordBreak: "break-all",
            }}
          >
            {agent.preparedRevision}
          </Text>
        </Field>
      ) : null}
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  const theme = useTheme();
  return (
    <div css={{ display: "grid", gap: theme.space(1) }}>
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
      {children}
    </div>
  );
}
