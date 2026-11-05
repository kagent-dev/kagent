import { Alert, Button, Card, Skeleton, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useTheme } from "@emotion/react";
import { CircleCheck, CircleDashed, TriangleAlert, Pencil } from "lucide-react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { RefreshButton } from "@/components/table/RefreshButton";
import { DeleteResourceButton } from "@/components/table/DeleteResourceButton";
import { linkInk } from "@/components/common/linkStyles";
import { agentPageUrl } from "@/components/agent/agentUrl";
import { egressCopy } from "@/components/egress-policy-form/egressPolicyCopy";
import {
  apiClient,
  isNotFound,
  policyUsers,
  useAgentsAcrossNamespaces,
  useEgressPolicy,
  useInvalidateEgressPolicies,
  useSandboxTemplates,
  type EgressAppliedState,
  type EgressDestination,
  type EgressPolicyUser,
} from "@/api";
import { buildPath, paths } from "@/router/routes";

const { Text } = Typography;

// region Helpers

function AppliedStatus({ user }: { user: EgressPolicyUser }) {
  const theme = useTheme();
  const look: Record<EgressAppliedState, { icon: typeof CircleCheck; color: string }> = {
    applied: { icon: CircleCheck, color: theme.color.successText },
    notApplied: { icon: TriangleAlert, color: theme.color.warningText },
    notReported: { icon: CircleDashed, color: theme.color.textMuted },
  };
  const { icon: Icon, color } = look[user.applied];
  return (
    <div data-testid={`egress-user-applied-${user.name}`}>
      <span css={{ display: "inline-flex", alignItems: "center", gap: theme.space(1.5), color }}>
        <Icon size={14} aria-hidden />
        {egressCopy.applied[user.applied]}
      </span>
      {user.detail ? (
        <Text css={{ display: "block", fontSize: 12, color: theme.color.textMuted }}>{user.detail}</Text>
      ) : null}
    </div>
  );
}

// region Component

export function EgressPolicyDetailsPage() {
  const theme = useTheme();
  const navigate = useNavigate();
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const policy = useEgressPolicy(namespace, name);
  const invalidate = useInvalidateEgressPolicies();
  const agents = useAgentsAcrossNamespaces(namespace ? [namespace] : undefined);
  const sandboxTemplates = useSandboxTemplates(namespace);
  const missing = policy.error !== undefined && isNotFound(policy.error);
  const users =
    namespace && name && agents.data && sandboxTemplates.data
      ? policyUsers({ namespace, name }, agents.data.agents, sandboxTemplates.data)
      : undefined;
  const usersError = agents.error ?? sandboxTemplates.error;
  const mono = { fontFamily: theme.font.mono };

  const destinationColumns: ColumnsType<EgressDestination> = [
    {
      title: egressCopy.destinationColumns.origin,
      key: "origin",
      render: (_, row) => <Text css={mono}>{row.origin}</Text>,
    },
    {
      title: egressCopy.destinationColumns.header,
      key: "header",
      render: (_, row) => (row.authentication ? <Text css={mono}>{row.authentication.header}</Text> : "—"),
    },
    {
      title: egressCopy.destinationColumns.prefix,
      key: "prefix",
      // Quoted, so a trailing space stays visible.
      render: (_, row) => (row.authentication?.prefix ? <Text css={mono}>"{row.authentication.prefix}"</Text> : "—"),
    },
    {
      title: egressCopy.destinationColumns.credential,
      key: "credential",
      render: (_, row) => {
        const auth = row.authentication;
        if (auth?.secretKeyRef) {
          return (
            <Space size={4} wrap>
              <Tag>{egressCopy.auth.secret}</Tag>
              <Text css={mono}>
                {auth.secretKeyRef.name}/{auth.secretKeyRef.key}
              </Text>
            </Space>
          );
        }
        if (auth?.actorJwt) {
          return (
            <Space size={4} wrap>
              <Tag>{egressCopy.auth.actorJwt}</Tag>
              <Text css={mono}>{auth.actorJwt.audiences.join(", ")}</Text>
            </Space>
          );
        }
        return <Text css={{ color: theme.color.textMuted }}>{egressCopy.details.noAuth}</Text>;
      },
    },
  ];

  const userColumns: ColumnsType<EgressPolicyUser> = [
    {
      title: egressCopy.usersColumns.name,
      key: "name",
      render: (_, user) => {
        const href = user.kind === "Agent" ? agentPageUrl(user) : undefined;
        return href ? (
          <Link to={href} css={{ ...mono, ...linkInk(theme) }} data-testid={`egress-user-link-${user.name}`}>
            {user.name}
          </Link>
        ) : (
          <Text css={mono}>{user.name}</Text>
        );
      },
    },
    { title: egressCopy.usersColumns.type, key: "kind", render: (_, user) => user.kind },
    { title: egressCopy.usersColumns.applied, key: "applied", render: (_, user) => <AppliedStatus user={user} /> },
  ];

  return (
    <PageFrame
      title={name ?? egressCopy.list.title}
      description={namespace && !missing ? egressCopy.details.description(namespace) : undefined}
      actions={
        <Space size={8} wrap>
          {policy.data && namespace && name ? (
            <>
              <Link to={buildPath(paths.egressPolicyEdit, { namespace, name })}>
                <Button icon={<Pencil size={14} />} data-testid="egress-policy-edit">
                  {egressCopy.details.edit}
                </Button>
              </Link>
              <DeleteResourceButton
                kind={egressCopy.kind}
                name={name}
                outlined
                confirmation="modal"
                label="Delete"
                // A refused namespace would undercount, so the warning falls back to the generic one.
                description={egressCopy.deleteWarning(agents.data?.refused.length ? undefined : users?.length)}
                onDelete={() => apiClient.egressPolicies.remove(namespace, name)}
                onDeleted={async () => {
                  await invalidate().catch(() => {});
                  await navigate(paths.egressPolicies);
                }}
              />
            </>
          ) : null}
          <Link to={paths.egressPolicies}>
            <Button>{egressCopy.details.back}</Button>
          </Link>
          <RefreshButton onRefresh={policy.refresh} what="Egress policy" loading={policy.isLoading} disabled={missing} />
        </Space>
      }
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
        {missing ? (
          <Alert
            type="warning"
            showIcon
            title={egressCopy.details.notFoundTitle}
            description={egressCopy.details.notFound(`${namespace}/${name}`)}
            data-testid="egress-policy-not-found"
            action={
              <Link to={paths.egressPolicies}>
                <Button size="small">{egressCopy.details.back}</Button>
              </Link>
            }
          />
        ) : policy.error ? (
          <Alert
            type="error"
            showIcon
            title={egressCopy.details.loadError}
            description={policy.error.message}
            data-testid="egress-policy-error"
            action={<Button size="small" onClick={() => void policy.refresh()}>Try again</Button>}
          />
        ) : null}

        {!missing && !policy.error ? (
          <Card title={egressCopy.details.destinations} data-testid="egress-policy-destinations">
            {policy.data ? (
              <Table<EgressDestination>
                rowKey="origin"
                size="small"
                columns={destinationColumns}
                dataSource={policy.data.resource.spec.destinations}
                pagination={false}
                scroll={{ x: "max-content" }}
              />
            ) : (
              <Skeleton active paragraph={{ rows: 3 }} data-testid="egress-policy-loading" />
            )}
          </Card>
        ) : null}

        {!missing && !policy.error ? (
          <Card title={egressCopy.details.usedBy} data-testid="egress-policy-users">
            <Text css={{ display: "block", marginBottom: theme.space(3), color: theme.color.textMuted }}>
              {egressCopy.details.usedByHelp} {egressCopy.usedByScope}
            </Text>
            {usersError ? (
              <Alert type="error" showIcon title={egressCopy.details.usedByError} description={usersError.message} />
            ) : (
              <Table<EgressPolicyUser>
                rowKey={(user) => `${user.kind}/${user.name}`}
                size="small"
                columns={userColumns}
                dataSource={users ?? []}
                loading={users === undefined}
                pagination={false}
                scroll={{ x: "max-content" }}
                locale={{ emptyText: users ? egressCopy.details.usedByEmpty : " " }}
              />
            )}
          </Card>
        ) : null}
      </Space>
    </PageFrame>
  );
}
