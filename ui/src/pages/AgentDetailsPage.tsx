import { Alert, Button, Card, Descriptions, Skeleton, Space, Tag, Typography } from "antd";
import type { DescriptionsProps } from "antd";
import { useTheme } from "@emotion/react";
import { Link, useParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import {
  NotReported,
  OperationTag,
  StateTag,
  ValueOrNotReported,
} from "@/components/agent-instances/InstanceTags";
import { LifecycleButton } from "@/components/agent-instances/LifecycleButton";
import {
  labelPairs,
  relativeAge,
  stateAppearance,
} from "@/components/agent-instances/instanceLabels";
import { buildPath, paths } from "@/router/routes";
import { AgentRail } from "@/components/agent/AgentRail";
import { agentPageUrl } from "@/components/agent/agentUrl";
import { bareName, isNotFound, useAgentInstance, useAgentInstances } from "@/api";

const { Paragraph, Text } = Typography;

/**
 * Everything this installation knows about one conversation, on one page.
 *
 * A conversation is an `AgentInstance`, so this is that record in full — one read,
 * because an instance is a row in the controller's database rather than a custom
 * resource with a spec to fetch.
 *
 * ## Where this sits now
 *
 * Between an agent and its chat. The agent — the `(AgentTemplate, Harness)` pair —
 * lists its conversations; this is one of them, and it links back up to the agent
 * rather than to the whole agents list. That is item 3 of the review, and the whole
 * of it: navigation, not a filter. "Agents filtered by this template" was circular
 * once an agent listed its own conversations, since it only ever meant "the other
 * conversations with this same agent".
 *
 * ## The failure gets a banner, not a row
 *
 * `Failure` is set only when the controller could not do what it intended, and it
 * is the reason someone opened this page. Putting it in the field list alongside
 * the A2A authority would bury the one thing that is wrong among nine things that
 * are fine.
 */
export function AgentDetailsPage() {
  const theme = useTheme();
  const { namespace, id } = useParams<{ namespace: string; id: string }>();
  const { data, isLoading, error, refresh } = useAgentInstance(namespace, id);
  /*
   * The other conversations with this agent, for the rail beside the page.
   *
   * Read here for the same reason the chat page reads them: the rail lists the
   * siblings of this instance, and a rail fetching its own copy would be a second
   * request for rows this page already has reason to hold.
   */
  const instances = useAgentInstances(namespace);

  /*
   * The agent this conversation belongs to, when the record names a pair.
   *
   * `undefined` is a real answer rather than a missing one: an instance with no
   * prepared revision belongs to no pair — the controller's own list query joins it
   * as NULL — so there is genuinely no agent page to link to, and rendering a link
   * anyway would point at one that does not exist.
   */
  const agentHref =
    data?.harness && data.agentTemplate
      ? agentPageUrl({
          namespace: data.namespace,
          agentTemplate: bareName(data.agentTemplate),
          harness: bareName(data.harness),
        })
      : undefined;

  // A 404 is a different answer from a failed request: the controller told us this
  // instance does not exist — or that it is not ours, which the API reports the same
  // way. Retrying would be pointless, so that branch offers a way back instead.
  const missing = error !== undefined && isNotFound(error);

  const fields: DescriptionsProps["items"] = data
    ? [
        {
          key: "id",
          label: "Instance ID",
          span: 2,
          children: (
            <Text copyable css={{ fontFamily: theme.font.mono, fontSize: 12 }}>
              {data.id}
            </Text>
          ),
        },
        {
          key: "namespace",
          label: "Namespace",
          children: <ValueOrNotReported value={data.namespace} mono />,
        },
        {
          key: "creator",
          label: "Creator",
          children: <ValueOrNotReported value={data.creator} />,
        },
        {
          key: "agent",
          label: "Agent",
          // Full width, and not only because it is the most important field here:
          // antd warns when the spans in a line do not sum to `column`, and that
          // warning fails the browser suite. An odd number of single-span fields is
          // therefore a test failure rather than a cosmetic one.
          span: 2,
          children: agentHref ? (
            <Link
              to={agentHref}
              data-testid="instance-agent-link"
              css={{ fontFamily: theme.font.mono, color: theme.color.primaryText }}
            >
              {`${bareName(data.agentTemplate ?? "")} on ${bareName(data.harness ?? "")}`}
            </Link>
          ) : (
            // Not a link and not a blank cell: an instance with no prepared revision
            // belongs to no pair, which is a fact about the record rather than a
            // link this page forgot to render.
            <ValueOrNotReported value={undefined} />
          ),
        },
        {
          key: "agentTemplate",
          label: "Agent template",
          children: data.agentTemplate ? (
            <Link
              to={buildPath(paths.agentTemplateDetail, {
                namespace: data.namespace,
                name: bareName(data.agentTemplate),
              })}
              data-testid="instance-template-link"
              css={{ fontFamily: theme.font.mono, color: theme.color.primaryText }}
            >
              {data.agentTemplate}
            </Link>
          ) : (
            <ValueOrNotReported value={undefined} />
          ),
        },
        {
          key: "harness",
          label: "Harness",
          // Not a link: `HarnessService` is read-only in this build, so there is no
          // harness page to open. The template is the half a reader can change.
          children: <ValueOrNotReported value={data.harness} mono />,
        },
        {
          key: "preparedRevision",
          label: "Prepared revision",
          children: <ValueOrNotReported value={data.preparedRevision} mono />,
        },
        {
          key: "a2aAuthority",
          label: "A2A authority",
          children: <ValueOrNotReported value={data.a2aAuthority} mono />,
        },
        {
          key: "createdAt",
          label: "Created",
          children: data.createdAt ? (
            <Text>
              {data.createdAt}{" "}
              <Text css={{ color: theme.color.textMuted }}>
                ({relativeAge(data.createdAt)})
              </Text>
            </Text>
          ) : (
            <NotReported />
          ),
        },
        {
          key: "updatedAt",
          label: "Last updated",
          children: data.updatedAt ? (
            <Text>
              {data.updatedAt}{" "}
              <Text css={{ color: theme.color.textMuted }}>
                ({relativeAge(data.updatedAt)})
              </Text>
            </Text>
          ) : (
            <NotReported />
          ),
        },
        {
          key: "labels",
          label: "Labels",
          span: 2,
          children:
            labelPairs(data).length > 0 ? (
              <Space size={4} wrap data-testid="instance-labels">
                {labelPairs(data).map((pair) => (
                  <Tag key={pair} css={{ fontFamily: theme.font.mono }}>
                    {pair}
                  </Tag>
                ))}
              </Space>
            ) : (
              // Distinct from "not reported": an instance with no labels is
              // ordinary, and saying the controller failed to mention them would
              // be wrong.
              <Text css={{ color: theme.color.textMuted }} data-testid="instance-no-labels">
                None set
              </Text>
            ),
        },
      ]
    : [];

  return (
    // No heading and no actions row.
    //
    // The rail beside this page already names the conversation, names its agent, and
    // carries the way back to both — so a title repeating the name, a subtitle
    // repeating the namespace, and a "Back to agent" button duplicating a rail entry
    // cost a band across the top of the page and said nothing new. Refresh went with
    // them: this page is a record, and the rail's own controls are what a reader
    // reaches for.
    <PageFrame
    >
      {/* The same rail as the chat, which is the point of it: leaving a conversation
          to read the agent's record should not take away the list of conversations
          you navigate by. */}
      <div
        data-testid="agent-surface"
        css={{
          display: "flex",
          gap: theme.space(6),
          /*
           * `flex-start`, not `stretch`.
           *
           * Stretched, each sidebar is as tall as the whole conversation — and a sticky
           * element as tall as its scroll container has nowhere to stick, so on a long
           * chat both rails scrolled away with the page. At `flex-start` they keep
           * their own height and stay put, which is the entire reason they are sticky.
           */
          alignItems: "flex-start",
        }}
      >
        {namespace && id ? (
          <AgentRail
            agentRef={{ namespace, id }}
            instance={data}
            instances={instances}
          />
        ) : null}

        <Space
          orientation="vertical"
          size="middle"
          css={{ display: "flex", flex: 1, minWidth: 0 }}
        >
        {missing ? (
          <Alert
            type="warning"
            showIcon
            title="This conversation cannot be opened"
            description={`No conversation ${id ?? ""} was found in ${namespace ?? "that namespace"}. It may have been deleted — or started by somebody else, which the controller reports in exactly the same words: an instance is read as its creator, so somebody else's answers "not found" rather than "not yours".`}
            data-testid="instance-not-found"
            action={
              <Link to={paths.agents}>
                <Button size="small">All agents</Button>
              </Link>
            }
          />
        ) : error ? (
          <Alert
            type="error"
            showIcon
            title="Could not load this conversation"
            description={error.message}
            data-testid="instance-error"
            action={
              <Button size="small" onClick={() => void refresh()}>
                Try again
              </Button>
            }
          />
        ) : null}

        {isLoading ? (
          <Skeleton active paragraph={{ rows: 6 }} data-testid="instance-loading" />
        ) : null}

        {data ? (
          <>
            <Card data-testid="instance-status-card">
              <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
                <Space size={12} wrap>
                  <StateTag state={data.state} testId="instance-state" />
                  <OperationTag operation={data.operation} testId="instance-operation" />
                </Space>

                <Paragraph
                  css={{ margin: 0, color: theme.color.textMuted }}
                  data-testid="instance-state-meaning"
                >
                  {stateAppearance(data.state).meaning}
                </Paragraph>

                {/*
                  One control, not two.
                  
                  Both actions used to be rendered side by side with whichever did not
                  apply left disabled — so a ready conversation showed a greyed-out
                  Resume, and a suspended one a greyed-out Suspend. A disabled control
                  the reader can never use in this state is not information; it is a
                  second thing to read past to find the one that works.
                  
                  The state decides which is offered. Anything that is neither ready nor
                  suspended — creating, failed, deleting — gets Suspend, disabled with
                  the controller's own reason, which is the honest answer: there is a
                  lifecycle action here and this state cannot take it.
                */}
                <Space size={8}>
                  <LifecycleButton
                    instance={data}
                    action={data.state === "suspended" ? "resume" : "suspend"}
                    onDone={refresh}
                    size="middle"
                    showLabel
                  />
                </Space>
              </Space>
            </Card>

            {data.failure ? (
              <Alert
                type="error"
                showIcon
                title={
                  data.failure.reason
                    ? `The controller reported a failure: ${data.failure.reason}`
                    : "The controller reported a failure"
                }
                /*
                 * A `Failure` can arrive with both halves empty — proto3 cannot tell
                 * an unset string from an empty one, so the message being present is
                 * the only signal there is. Saying so is better than an alert with
                 * nothing in it, which reads as a rendering bug rather than as a
                 * gap in the record.
                 */
                description={
                  data.failure.message ??
                  "The record carries a failure with no reason or message in it. That is the whole of what the controller stored."
                }
                data-testid="instance-failure"
              />
            ) : null}

            <Card title="Details" data-testid="instance-details-card">
              <Descriptions
                bordered
                column={2}
                size="small"
                items={fields}
                data-testid="instance-details"
              />
              <Paragraph
                css={{
                  margin: `${theme.space(4)} 0 0`,
                  color: theme.color.textMuted,
                  fontSize: 12,
                }}
                data-testid="instance-template-note"
              >
                The agent is the template and the harness together, and it is what
                this conversation was cut from. Its page lists the other
                conversations with it. Editing the template changes every agent made
                from it, not only this one — which is why the link goes to the
                template rather than pretending it belongs to this conversation.
              </Paragraph>
            </Card>
          </>
        ) : null}
        </Space>
      </div>
    </PageFrame>
  );
}
