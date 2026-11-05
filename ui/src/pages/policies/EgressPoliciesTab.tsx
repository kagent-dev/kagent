import { useMemo } from "react";
import { Alert, Button, Space, Table, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Pencil } from "lucide-react";
import { useTheme } from "@emotion/react";
import { Link } from "react-router-dom";
import { DeleteResourceButton } from "@/components/table/DeleteResourceButton";
import { RefreshButton } from "@/components/table/RefreshButton";
import { FilterBar } from "@/components/table/FilterBar";
import { useListView } from "@/components/table/useListView";
import { byNumber, byText, listTableChange, matchesQuery, paginationFor, sortOrderFor } from "@/components/table/listTable";
import { linkInk } from "@/components/common/linkStyles";
import { egressCopy } from "@/components/egress-policy-form/egressPolicyCopy";
import {
  apiClient,
  credentialSources,
  policyUsers,
  useAgentsAcrossNamespaces,
  useEgressPolicies,
  useInvalidateEgressPolicies,
  useSandboxTemplates,
  type EgressPolicy,
} from "@/api";
import { buildPath, paths } from "@/router/routes";

const { Text } = Typography;

// region Helpers

const FILTER_IDS: readonly string[] = ["ns"];
const PAGE_SIZE = 25;

interface PolicyRow {
  policy: EgressPolicy;
  key: string;
  origins: string[];
  auth: string;
  /** Undefined until both user lists have loaded. */
  usedBy?: number;
}

// region Component

export function EgressPoliciesTab() {
  const theme = useTheme();
  const view = useListView(FILTER_IDS);
  const policies = useEgressPolicies();
  const invalidate = useInvalidateEgressPolicies();
  // Policies apply only in their own namespace, so only those namespaces are read.
  const policyNamespaces = useMemo(
    () => (policies.data ? [...new Set(policies.data.map((policy) => policy.namespace))] : undefined),
    [policies.data],
  );
  const agents = useAgentsAcrossNamespaces(policyNamespaces);
  const sandboxTemplates = useSandboxTemplates();
  const usersFailed = Boolean(agents.error || sandboxTemplates.error);

  const rows: PolicyRow[] = useMemo(
    () =>
      (policies.data ?? []).map((policy) => {
        const sources = credentialSources(policy);
        return {
          policy,
          key: policy.ref,
          origins: policy.resource.spec.destinations.map((entry) => entry.origin),
          auth: sources.length ? sources.map((source) => egressCopy.auth[source]).join(", ") : egressCopy.auth.none,
          // A refused namespace would undercount its policies, so they show no count.
          usedBy:
            agents.data &&
            sandboxTemplates.data &&
            !agents.data.refused.some((entry) => entry.namespace === policy.namespace)
              ? policyUsers(policy, agents.data.agents, sandboxTemplates.data).length
              : undefined,
        };
      }),
    [policies.data, agents.data, sandboxTemplates.data],
  );

  const namespaceOptions = useMemo(
    () => [...(policyNamespaces ?? [])].sort().map((value) => ({ value })),
    [policyNamespaces],
  );
  const selected = view.selected("ns");
  const filtered = rows.filter(
    (row) =>
      (selected.length === 0 || selected.includes(row.policy.namespace)) &&
      matchesQuery(view.query, [row.policy.name, ...row.origins]),
  );

  const columns: ColumnsType<PolicyRow> = [
    {
      title: egressCopy.columns.name,
      key: "name",
      sorter: byText<PolicyRow>((row) => row.policy.name),
      sortOrder: sortOrderFor(view, "name"),
      render: (_, row) => (
        <Link
          to={buildPath(paths.egressPolicyDetail, { namespace: row.policy.namespace, name: row.policy.name })}
          css={{ fontFamily: theme.font.mono, ...linkInk(theme) }}
        >
          {row.policy.name}
        </Link>
      ),
    },
    {
      title: egressCopy.columns.namespace,
      key: "namespace",
      sorter: byText<PolicyRow>((row) => row.policy.namespace),
      sortOrder: sortOrderFor(view, "namespace"),
      render: (_, row) => row.policy.namespace,
    },
    {
      title: egressCopy.columns.origins,
      key: "origins",
      sorter: byNumber<PolicyRow>((row) => row.origins.length),
      sortOrder: sortOrderFor(view, "origins"),
      render: (_, row) => row.origins.length,
    },
    { title: egressCopy.columns.auth, key: "auth", render: (_, row) => row.auth },
    {
      title: egressCopy.columns.usedBy,
      key: "usedBy",
      sorter: byNumber<PolicyRow>((row) => row.usedBy ?? -1),
      sortOrder: sortOrderFor(view, "usedBy"),
      render: (_, row) =>
        row.usedBy === undefined ? <Text css={{ color: theme.color.textMuted }}>—</Text> : row.usedBy,
    },
    {
      title: "",
      key: "actions",
      width: 76,
      render: (_, row) => (
        <Space size={0}>
          <Link to={buildPath(paths.egressPolicyEdit, { namespace: row.policy.namespace, name: row.policy.name })}>
            <Button
              type="text"
              size="small"
              icon={<Pencil size={14} />}
              data-testid={`edit-${row.policy.name}`}
              aria-label={`Edit ${egressCopy.kind} ${row.policy.name}`}
            />
          </Link>
          <DeleteResourceButton
            kind={egressCopy.kind}
            name={row.policy.ref}
            description={egressCopy.deleteWarning(row.usedBy)}
            onDelete={() => apiClient.egressPolicies.remove(row.policy.namespace, row.policy.name)}
            onDeleted={invalidate}
          />
        </Space>
      ),
    },
  ];

  return (
    <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
      {policies.error ? (
        <Alert
          type="error"
          showIcon
          title={egressCopy.list.loadError}
          description={policies.error.message}
          data-testid="egress-policies-error"
          action={<Button size="small" onClick={() => void policies.refresh()}>Try again</Button>}
        />
      ) : null}
      {!policies.error && usersFailed ? (
        <Alert type="warning" showIcon title={egressCopy.list.usersError} data-testid="egress-policies-users-error" />
      ) : null}

      <FilterBar
        testId="egress-policies-filters"
        view={view}
        search={{ label: egressCopy.list.search, placeholder: egressCopy.list.search }}
        filters={[{ id: "ns", label: "Namespace", allLabel: "All namespaces", options: namespaceOptions }]}
        trailing={<RefreshButton onRefresh={invalidate} what="Egress policies" loading={policies.isLoading} />}
      />

      <Text css={{ color: theme.color.textMuted, fontSize: 12 }} data-testid="egress-policies-used-by-scope">
        {egressCopy.usedByScope}
      </Text>

      <Table<PolicyRow>
        data-testid="egress-policies-table"
        rowKey="key"
        columns={columns}
        dataSource={policies.error ? [] : filtered}
        loading={policies.isLoading}
        scroll={{ x: "max-content" }}
        onChange={listTableChange<PolicyRow>(view)}
        pagination={paginationFor(view, filtered.length, PAGE_SIZE)}
        locale={{
          emptyText: policies.isEmpty ? egressCopy.list.empty : view.isNarrowed ? egressCopy.list.noMatch : " ",
        }}
      />
    </Space>
  );
}
