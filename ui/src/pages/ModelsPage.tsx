import { useMemo } from "react";
import { Link } from "react-router-dom";
import { Pencil, Plus } from "lucide-react";
import { Alert, Button, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useTheme } from "@emotion/react";
import { PageFrame } from "@/components/Structure/PageFrame";
import { buildPath, paths } from "@/router/routes";
import { apiClient, parseRef, useModels, type ModelConfig } from "@/api";
import { DeleteResourceButton } from "@/components/table/DeleteResourceButton";
import { RefreshButton } from "@/components/table/RefreshButton";
import { FilterBar, WholeListNote } from "@/components/table/FilterBar";
import { useListView } from "@/components/table/useListView";
import {
  byText,
  listTableChange,
  matchesQuery,
  paginationFor,
  sortOrderFor,
} from "@/components/table/listTable";

const { Text } = Typography;

/** The filters this page offers, by the parameter each is remembered in. */
const FILTERS = ["ns", "provider"] as const;
const FILTER_IDS: readonly string[] = FILTERS;

const PAGE_SIZE = 25;

/**
 * Model configurations.
 *
 * ## Where the narrowing happens, and why it is honest here
 *
 * `ListModelConfigs` takes an **empty request**: no page, no filter, no sort. The
 * whole list arrives in one message, so every row is already in the browser and a
 * search here searches all of them. That is what makes a client-side control truthful
 * on this page and a lie on the substrate page's actor table, which is paged by the
 * server — there, filtering what was fetched would report "no matches" about a row on
 * page nine, which is why those columns offer no sort at all.
 *
 * The page says so itself rather than leaving a reader to assume; `playwright/DEFERRED.md`
 * records the API change the RPC needs before any of this can move server-side.
 *
 * ## What is new here
 *
 * This page had no search box at all — the only way to find a configuration among a
 * cluster's worth of them was to read the table. It now has one, a namespace and a
 * provider filter, a sort on every column and a page control, all held in the URL so
 * a narrowed view can be linked to and survives a reload.
 */
export function ModelsPage() {
  const theme = useTheme();
  const { data, isLoading, error, isEmpty, refresh } = useModels();
  const view = useListView(FILTER_IDS);

  const models = useMemo(() => data ?? [], [data]);

  /*
   * The filter options come from the rows themselves rather than from the namespace
   * list, because these are the only namespaces that can match: offering one holding
   * no model configurations gives the reader a choice whose only outcome is an empty
   * table.
   */
  const namespaceOptions = useMemo(
    () =>
      [...new Set(models.map((model) => parseRef(model.ref).namespace))]
        .filter(Boolean)
        .sort()
        .map((value) => ({ value })),
    [models],
  );

  const providerOptions = useMemo(
    () =>
      [...new Set(models.map((model) => model.spec.provider))]
        .filter(Boolean)
        .sort()
        .map((value) => ({ value })),
    [models],
  );

  const selectedNamespaces = view.selected("ns");
  const selectedProviders = view.selected("provider");

  const filtered = useMemo(
    () =>
      models.filter((model) => {
        const { namespace, name } = parseRef(model.ref);
        // Nothing chosen means every namespace, which is the default the page opens
        // in — not "no namespaces", which would show an empty table.
        if (selectedNamespaces.length > 0 && !selectedNamespaces.includes(namespace)) {
          return false;
        }
        if (
          selectedProviders.length > 0 &&
          !selectedProviders.includes(model.spec.provider)
        ) {
          return false;
        }
        // Every column the row shows is searchable, so a term a reader can see on
        // screen is a term that finds the row.
        return matchesQuery(view.query, [
          name,
          namespace,
          model.spec.provider,
          model.spec.model,
          model.spec.apiKeySecret,
        ]);
      }),
    [models, selectedNamespaces, selectedProviders, view.query],
  );

  const columns = useMemo<ColumnsType<ModelConfig>>(
    () => [
      {
        title: "Name",
        key: "name",
        sorter: byText<ModelConfig>((row) => parseRef(row.ref).name),
        sortOrder: sortOrderFor(view, "name"),
        render: (_, row) => parseRef(row.ref).name,
      },
      {
        title: "Namespace",
        key: "namespace",
        sorter: byText<ModelConfig>((row) => parseRef(row.ref).namespace),
        sortOrder: sortOrderFor(view, "namespace"),
        render: (_, row) => parseRef(row.ref).namespace || "—",
      },
      {
        title: "Provider",
        key: "provider",
        sorter: byText<ModelConfig>((row) => row.spec.provider),
        sortOrder: sortOrderFor(view, "provider"),
        render: (_, row) => <Tag>{row.spec.provider}</Tag>,
      },
      {
        title: "Model",
        key: "model",
        sorter: byText<ModelConfig>((row) => row.spec.model),
        sortOrder: sortOrderFor(view, "model"),
        render: (_, row) => row.spec.model,
      },
      {
        title: "API key secret",
        key: "secret",
        sorter: byText<ModelConfig>((row) => row.spec.apiKeySecret ?? ""),
        sortOrder: sortOrderFor(view, "secret"),
        render: (_, row) => row.spec.apiKeySecret ?? "—",
      },
      {
        title: "",
        key: "actions",
        width: 48,
        render: (_, row) => {
          const { namespace, name } = parseRef(row.ref);
          return (
            <Space size={0}>
              <Link to={buildPath(paths.modelEdit, { namespace, name })}>
                <Button
                  type="text"
                  size="small"
                  icon={<Pencil size={14} />}
                  data-testid={`edit-${name}`}
                  aria-label={`Edit model configuration ${name}`}
                />
              </Link>
              <DeleteResourceButton
                kind="model configuration"
                name={name}
                onDelete={() => apiClient.models.remove(namespace, name)}
                onDeleted={refresh}
              />
            </Space>
          );
        },
      },
    ],
    [refresh, view],
  );

  return (
    <PageFrame
      title="Models"
      description="Model configurations available to agents."
      actions={
        <Space size={8}>
          <RefreshButton onRefresh={refresh} what="Models" loading={isLoading} />
          {/* The same page-level create the tool server and prompt lists carry. It
              matters more than it looks: the header's create menu is part of the default
              shell, and a distribution that supplies its own layout does not inherit it —
              leaving `/models/new` reachable only by typing the URL. An action on the
              list itself belongs to the page, so it survives whatever frames it. */}
          <Link to={paths.modelNew}>
            <Button type="primary" icon={<Plus size={14} />} data-testid="models-new">
              New model
            </Button>
          </Link>
        </Space>
      }
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
        {error ? (
          <Alert
            type="error"
            showIcon
            title="Could not load model configurations"
            description={error.message}
            data-testid="models-error"
            action={
              <Button size="small" onClick={() => void refresh()}>
                Try again
              </Button>
            }
          />
        ) : null}

        <FilterBar
          testId="models-filters"
          view={view}
          search={{
            label: "Search model configurations",
            placeholder: "Search names, providers, models and secrets",
          }}
          filters={[
            {
              id: "ns",
              label: "Namespace",
              allLabel: "All namespaces",
              options: namespaceOptions,
            },
            {
              id: "provider",
              label: "Provider",
              allLabel: "All providers",
              options: providerOptions,
            },
          ]}
          trailing={
            // Only a successful load can be counted. Saying "0 of 0" because a
            // request failed would be a claim the page cannot support.
            !error && !isLoading ? (
              <Text data-testid="models-summary" css={{ color: theme.color.textMuted }}>
                {filtered.length} of {models.length}{" "}
                {models.length === 1 ? "configuration" : "configurations"}
              </Text>
            ) : null
          }
        />

        <Table<ModelConfig>
          data-testid="models-table"
          rowKey={(row) => row.ref}
          columns={columns}
          dataSource={error ? [] : filtered}
          loading={isLoading}
          onChange={listTableChange<ModelConfig>(view)}
          pagination={paginationFor(view, filtered.length, PAGE_SIZE)}
          locale={{
            emptyText: isEmpty
              ? "No model configurations yet."
              : view.isNarrowed && !error
                ? "No model configurations match those filters."
                : " ",
          }}
        />

        <WholeListNote testId="models-read-note" rpc="ListModelConfigs" />
      </Space>
    </PageFrame>
  );
}
