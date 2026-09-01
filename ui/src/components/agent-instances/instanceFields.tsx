import { Button, Space, Tag, Tooltip, Typography } from "antd";
import type { DescriptionsProps } from "antd";
import type { Theme } from "@emotion/react";
import { Pencil } from "lucide-react";
import { Link } from "react-router-dom";
import { ValueOrNotReported } from "@/components/agent-instances/InstanceTags";
import { bareName } from "@/api";
import {
  labelPairs,
  relativeAge,
  shortInstanceId,
} from "@/components/agent-instances/instanceLabels";
import { NotReported } from "@/components/agent-instances/InstanceTags";
import { buildPath, paths } from "@/router/routes";
import type { AgentInstance } from "@/api";

const { Text } = Typography;

/**
 * One conversation's record, as rows.
 *
 * Extracted so the page and the modal that replaced its entry in the rail render the
 * same thing. Two copies of a record drift, and the one nobody opens is the one that
 * stops showing a field the controller started sending.
 */
export function instanceFields(
  data: AgentInstance,
  theme: Theme,
  /** Where the agent's own page is, when this record names a pair. */
  agentHref?: string,
  /**
   * Offered where the reader can act on it, and omitted where they cannot.
   *
   * The name is the one field on this record a reader owns, so it is the one field worth
   * an edit control — and it is first, because it is what they came to check. Absent, the
   * row still shows the name; it is the pencil that is conditional, not the fact.
   */
  onRename?: () => void,
): DescriptionsProps["items"] {
  return [
  {
    key: "name",
    label: "Name",
    span: 2,
    children: (
      <Space size={4}>
        {data.name ? (
          <Text>{data.name}</Text>
        ) : (
          // Not a gap: an unnamed conversation is the ordinary case, and it is
          // identified by its id everywhere else in the app. Saying "not reported"
          // here would read as the controller having failed to send something.
          <Text css={{ color: theme.color.textMuted }}>
            Untitled · {shortInstanceId(data.id)}
          </Text>
        )}
        {onRename ? (
          <Tooltip title="Rename this conversation">
            <Button
              type="text"
              size="small"
              icon={<Pencil size={13} />}
              onClick={onRename}
              data-testid="conversation-details-rename"
              aria-label="Rename this conversation"
            />
          </Tooltip>
        ) : null}
      </Space>
    ),
  },
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
];
}
