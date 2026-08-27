import { useMemo } from "react";
import { Alert, Button, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Pencil, Plus } from "lucide-react";
import { useTheme } from "@emotion/react";
import { Link } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { buildPath, paths } from "@/router/routes";
import { apiClient, useNamespaces, usePrompts, type PromptTemplateSummary } from "@/api";
import { DeleteResourceButton } from "@/components/table/DeleteResourceButton";
import { RefreshButton } from "@/components/table/RefreshButton";
import { FilterBar } from "@/components/table/FilterBar";
import { useListView } from "@/components/table/useListView";
import {
  byNumber,
  byText,
  listTableChange,
  matchesQuery,
  paginationFor,
  sortOrderFor,
} from "@/components/table/listTable";

const { Text } = Typography;

const FILTER_IDS: readonly string[] = ["ns"];
const PAGE_SIZE = 25;

/**
 * Prompt libraries.
 *
 * ## The namespace filter here is the one that is genuinely server-side
 *
 * `ListPromptTemplates` takes a namespace and rejects a request without one, so this
 * page never had a single "everything" read to narrow: `usePrompts` fans out, one
 * call per namespace. Choosing two namespaces is therefore two reads of exactly those
 * two — the server does the narrowing, and nothing is fetched that the reader did not
 * ask for.
 *
 * Search and sort are a different matter. The request carries no search term and no
 * order, so both happen in the browser — which is truthful here because whatever
 * namespaces are in scope arrive whole, so a search covers every row in scope rather
 * than one page of it. The note under the table says as much, and
 * `playwright/DEFERRED.md` records what the RPC would need to move the rest across.
 */
export function PromptsPage() {
  const theme = useTheme();
  const view = useListView(FILTER_IDS);

  // The namespaces to read. Empty means every one the app can see, which is what
  // `usePrompts` does with no argument.
  const selectedNamespaces = view.selected("ns");
  const { data, isLoading, error, isEmpty, refresh } = usePrompts(selectedNamespaces);

  /*
   * The options come from the app's namespace list rather than from the rows, because
   * the rows on screen are already narrowed by this very filter: deriving the options
   * from them would leave the reader unable to choose a second namespace once they
   * had chosen a first, and unable to get back to a namespace they had deselected.
   */
  const namespaces = useNamespaces();
  const namespaceOptions = useMemo(
    () => (namespaces.data ?? []).map((entry) => ({ value: entry.name })),
    [namespaces.data],
  );

  const libraries = useMemo(() => data ?? [], [data]);

  const filtered = useMemo(
    () =>
      libraries.filter((library) =>
        // Keys as well as names: somebody hunting a fragment usually knows what it is
        // called inside the library, not which library it ended up in.
        matchesQuery(view.query, [
          library.name,
          library.namespace,
          ...(library.keys ?? []),
        ]),
      ),
    [libraries, view.query],
  );

  const columns: ColumnsType<PromptTemplateSummary> = useMemo(
    () => [
      {
        title: "Name",
        key: "name",
        sorter: byText<PromptTemplateSummary>((row) => row.name),
        sortOrder: sortOrderFor(view, "name"),
        render: (_, row) => (
          <Link
            to={buildPath(paths.promptDetail, {
              namespace: row.namespace,
              name: row.name,
            })}
            // The table's own text colour would otherwise win here, leaving the
            // only navigable cell on the row looking like plain text. `primaryText`
            // rather than `primary`: the latter is a surface colour, and as ink on the
            // dark theme's page it measured 2.5:1.
            css={{ fontFamily: theme.font.mono, color: theme.color.primaryText }}
          >
            {row.name}
          </Link>
        ),
      },
      {
        title: "Namespace",
        key: "namespace",
        sorter: byText<PromptTemplateSummary>((row) => row.namespace),
        sortOrder: sortOrderFor(view, "namespace"),
        render: (_, row) => row.namespace || "—",
      },
      {
        title: "Keys",
        key: "keyCount",
        sorter: byNumber<PromptTemplateSummary>((row) => row.keyCount),
        sortOrder: sortOrderFor(view, "keyCount"),
        render: (_, row) => `${row.keyCount} ${row.keyCount === 1 ? "key" : "keys"}`,
      },
      {
        // Editing and deleting were both in the client, both unit-tested, and
        // unreachable from anywhere in the app — so a library could be created and
        // read and then never changed or removed.
        title: "",
        key: "actions",
        width: 76,
        render: (_, row) => (
          <Space size={0}>
            <Link
              to={buildPath(paths.promptDetail, {
                namespace: row.namespace,
                name: row.name,
              })}
            >
              <Button
                type="text"
                size="small"
                icon={<Pencil size={14} />}
                data-testid={`edit-${row.name}`}
                aria-label={`Edit prompt library ${row.name}`}
              />
            </Link>
            <DeleteResourceButton
              kind="prompt library"
              name={row.name}
              onDelete={() => apiClient.prompts.remove(row.namespace, row.name)}
              onDeleted={refresh}
            />
          </Space>
        ),
      },
      {
        title: "Fragments",
        key: "keys",
        render: (_, row) =>
          row.keys?.length ? (
            <Space size={4} wrap>
              {row.keys.map((key) => (
                <Tag key={key} css={{ fontFamily: theme.font.mono, marginInlineEnd: 0 }}>
                  {key}
                </Tag>
              ))}
            </Space>
          ) : (
            "—"
          ),
      },
    ],
    [theme, refresh, view],
  );

  return (
    <PageFrame
      title="Prompts"
      description={
        /*
         * What to do, in the order you have to do it.
         *
         * The old wording gave the template call and stopped there, which left out the
         * step that makes it work: a library is only reachable from an agent that lists
         * it. Anyone following the sentence as written got the controller's own error —
         * "prompt template not found in promptSources".
         *
         * The name in the path is the source's alias, or its resource name when it has no
         * alias, and the key is an entry inside the library. Both come from the
         * controller's own resolution — see `resolvePromptSources` — rather than from a
         * guess about the syntax.
         */
        'Reusable prompt fragments. List a library under an agent\'s promptSources, then include one of its fragments in that agent\'s instructions with {{include "library/key"}} — where "library" is the alias you gave the source (or its name) and "key" is the fragment.'
      }
      actions={
        <Space size={8}>
          <RefreshButton onRefresh={refresh} what="Prompt libraries" loading={isLoading} />
          <Link to={paths.promptNew}>
            <Button type="primary" icon={<Plus size={14} />} data-testid="prompts-new">
              New library
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
            title="Could not load prompt libraries"
            description={error.message}
            data-testid="prompts-error"
            action={
              <Button size="small" onClick={() => void refresh()}>
                Try again
              </Button>
            }
          />
        ) : null}

        <FilterBar
          testId="prompts-filters"
          view={view}
          search={{
            label: "Search libraries and fragment keys",
            placeholder: "Search libraries and fragment keys",
          }}
          filters={[
            {
              id: "ns",
              label: "Namespace",
              allLabel: "All namespaces",
              options: namespaceOptions,
            },
          ]}
          trailing={
            !error && !isLoading ? (
              <Text data-testid="prompts-summary" css={{ color: theme.color.textMuted }}>
                {filtered.length} of {libraries.length}{" "}
                {libraries.length === 1 ? "library" : "libraries"}
                {/* "Read" rather than "in total": with a namespace chosen the second
                    number is what those namespaces hold, and the page has not asked
                    the cluster about the rest — so it cannot claim a total. */}
                {selectedNamespaces.length > 0 ? " read" : ""}
              </Text>
            ) : null
          }
        />

        <Table<PromptTemplateSummary>
          data-testid="prompts-table"
          rowKey={(row) => `${row.namespace}/${row.name}`}
          columns={columns}
          // A failure already has its own message above. Leaving rows out of the
          // table as well keeps it from claiming "there are no libraries", which
          // is a different thing from "we could not find out".
          dataSource={error ? [] : filtered}
          loading={isLoading}
          onChange={listTableChange<PromptTemplateSummary>(view)}
          pagination={paginationFor(view, filtered.length, PAGE_SIZE)}
          locale={{
            // Narrowed first, unlike the other lists: the read here is *scoped* by
            // the namespace filter, so "nothing came back" does not mean there are no
            // libraries — only none in the namespaces asked for. Saying "no prompt
            // libraries yet" to somebody who has filtered to an empty namespace would
            // be false about the cluster.
            emptyText:
              view.isNarrowed && !error
                ? "No prompt libraries match those filters."
                : isEmpty
                  ? "No prompt libraries yet."
                  : " ",
          }}
        />
      </Space>
    </PageFrame>
  );
}
