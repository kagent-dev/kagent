import type { ReactNode } from "react";
import { Alert, Button, Card, Descriptions, Skeleton, Space, Table, Tag, Typography } from "antd";
import type { DescriptionsProps } from "antd";
import { useTheme } from "@emotion/react";
import { Pencil } from "lucide-react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { RefreshButton } from "@/components/table/RefreshButton";
import { BackButton } from "@/components/common/BackButton";
import { useGoBack } from "@/router/backLink";
import { ResourceLink } from "@/components/common/ResourceLink";
import { resourceIcons } from "@/components/common/resourceIcons";
import { agentPageUrl } from "@/components/agent/agentUrl";
import { templateRefName, useAgentsAcrossNamespaces, useHarnesses, type Agent, type HarnessSpec } from "@/api";
import { backLinkState } from "@/router/backLink";
import { buildPath, harnessesTab, paths } from "@/router/routes";

const { Text } = Typography;

/**
 * One harness, read-only. `HarnessService` has no get, so it is found in its
 * namespace's list, the same read the edit page makes.
 */
export function HarnessDetailsPage() {
  const theme = useTheme();
  const location = useLocation();
  const navigate = useNavigate();
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const back = useGoBack({ to: harnessesTab, label: "Harnesses" });
  const harnesses = useHarnesses(namespace);
  const harness = harnesses.data?.find((row) => row.namespace === namespace && row.name === name);
  const missing = Boolean(harnesses.data) && !harness;
  const agents = useAgentsAcrossNamespaces(namespace ? [namespace] : undefined);
  const users = (agents.data?.agents ?? []).filter((agent) => agent.resource.spec.harnessRef.name === name);

  // The read is opaque by design; these are the fields the CRD defines today.
  const spec = harness?.resource.spec as Partial<HarnessSpec> | undefined;
  const mono = { fontFamily: theme.font.mono, fontSize: 12 };
  const here = backLinkState(location, "Harness");
  const modelLink = (ref: { name: string; namespace?: string } | undefined, testId: string): ReactNode =>
    ref ? (
      <ResourceLink
        mono
        css={{ fontSize: 12 }}
        to={buildPath(paths.modelEdit, { namespace: ref.namespace || namespace || "", name: ref.name })}
        state={here}
        icon={resourceIcons.model}
        data-testid={testId}
      >
        {`${ref.namespace || namespace}/${ref.name}`}
      </ResourceLink>
    ) : null;

  const memoryModel = modelLink(spec?.kagent?.memory?.modelConfigRef, "harness-memory-model");
  const summarizerModel = modelLink(spec?.kagent?.compaction?.summarizer?.modelConfigRef, "harness-summarizer-model");
  const fields: DescriptionsProps["items"] = harness
    ? [
        { key: "runtime", label: "Runtime", children: <Tag data-testid="harness-runtime">{harness.runtime || "Not reported"}</Tag> },
        {
          key: "image",
          label: "Workload image",
          children: harness.workloadImage ? (
            <Text copyable css={{ ...mono, wordBreak: "break-all" }}>{harness.workloadImage}</Text>
          ) : (
            <Text css={{ color: theme.color.textMuted }}>Release default</Text>
          ),
        },
        { key: "pool", label: "Worker pool", children: <Text css={mono}>{spec?.substrate?.workerPoolRef?.name || "—"}</Text> },
        { key: "snapshots", label: "Snapshot location", children: <Text css={mono}>{spec?.substrate?.snapshotPolicy?.location || "—"}</Text> },
        ...(memoryModel ? [{ key: "memory", label: "Memory model", children: memoryModel }] : []),
        ...(summarizerModel ? [{ key: "summarizer", label: "Summary model", children: summarizerModel }] : []),
        ...(spec?.env?.length
          ? [{
              key: "env",
              label: "Environment",
              children: (
                <Space size={4} wrap>
                  {spec.env.map((entry) => <Tag key={entry.name} css={{ fontFamily: theme.font.mono }}>{entry.name}</Tag>)}
                </Space>
              ),
            }]
          : []),
      ]
    : [];

  return (
    <PageFrame
      title={name ? `Harness ${name}` : "Harness"}
      icon={resourceIcons.harness}
      description={namespace && !missing ? `The runtime agents run on, in the ${namespace} namespace.` : undefined}
      actions={
        <Space size={8} wrap>
          {harness && namespace && name ? (
            <Button
              icon={<Pencil size={14} />}
              data-testid="harness-edit"
              onClick={() => navigate(buildPath(paths.harnessEdit, { namespace, name }), { state: here })}
            >
              Edit
            </Button>
          ) : null}
          <BackButton back={back} testId="harness-back" />
          <RefreshButton onRefresh={harnesses.refresh} what="Harness" loading={harnesses.isLoading} disabled={missing} iconOnly />
        </Space>
      }
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex", maxWidth: 900 }}>
        {harnesses.error ? (
          <Alert
            type="error"
            showIcon
            title="Could not load this harness"
            description={harnesses.error.message}
            data-testid="harness-detail-error"
            action={<Button size="small" onClick={() => void harnesses.refresh()}>Try again</Button>}
          />
        ) : missing ? (
          <Alert
            type="warning"
            showIcon
            title="This harness does not exist"
            description={`No harness named ${namespace}/${name} was found. It may have been deleted.`}
            data-testid="harness-detail-not-found"
          />
        ) : null}

        {harness ? (
          <Descriptions bordered column={1} size="small" items={fields} styles={{ label: { width: 180 } }} data-testid="harness-details" />
        ) : !harnesses.error && !missing ? (
          <Skeleton active paragraph={{ rows: 5 }} data-testid="harness-detail-loading" />
        ) : null}

        {harness ? (
          <Card size="small" title="Used by" data-testid="harness-users">
            {agents.error ? (
              <Text css={{ color: theme.color.textMuted }}>Could not read the agents in this namespace.</Text>
            ) : !agents.data ? (
              <Skeleton active paragraph={{ rows: 2 }} title={false} />
            ) : users.length === 0 ? (
              <Text css={{ color: theme.color.textMuted }}>No agents in this namespace use this harness.</Text>
            ) : (
              <Table<Agent>
                size="small"
                rowKey={(row) => row.ref}
                dataSource={users}
                pagination={users.length > 10 ? { pageSize: 10, hideOnSinglePage: true } : false}
                columns={[
                  {
                    title: "Agent",
                    key: "agent",
                    render: (_, agent) => (
                      <ResourceLink
                        mono
                        css={{ fontSize: 12 }}
                        to={agentPageUrl(agent) ?? paths.agents}
                        state={here}
                        icon={resourceIcons.agent}
                        data-testid={`harness-user-${agent.name}`}
                      >
                        {agent.name}
                      </ResourceLink>
                    ),
                  },
                  {
                    title: "Template",
                    key: "template",
                    render: (_, agent) => {
                      const template = templateRefName(agent);
                      return template ? (
                        <ResourceLink
                          mono
                          css={{ fontSize: 12 }}
                          to={buildPath(paths.agentTemplateDetail, { namespace: agent.namespace, name: template })}
                          state={here}
                          icon={resourceIcons.template}
                          data-testid={`harness-user-template-${agent.name}`}
                        >
                          {template}
                        </ResourceLink>
                      ) : (
                        <Tag css={{ marginInlineEnd: 0 }} data-testid={`harness-user-template-${agent.name}`}>
                          Inline
                        </Tag>
                      );
                    },
                  },
                ]}
              />
            )}
          </Card>
        ) : null}
      </Space>
    </PageFrame>
  );
}
