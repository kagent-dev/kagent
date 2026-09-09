import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { Alert, Button, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Trash2 } from "lucide-react";
import { useTheme } from "@emotion/react";
import toast from "react-hot-toast";
import { PageFrame } from "@/components/Structure/PageFrame";
import { apiClient, useSnapshots, type Snapshot } from "@/api";
import { agentUrl } from "@/components/agent/agentUrl";
import {
  conversationTitle,
  relativeAge,
  shortInstanceId,
} from "@/components/agent-instances/instanceLabels";
import { DeleteResourceButton } from "@/components/table/DeleteResourceButton";
import { RefreshButton } from "@/components/table/RefreshButton";
import { FilterBar } from "@/components/table/FilterBar";
import { useListView } from "@/components/table/useListView";
import {
  byText,
  listTableChange,
  matchesQuery,
  paginationFor,
  sortOrderFor,
} from "@/components/table/listTable";
import { deleteSnapshots, deletedMessage } from "./deleteSnapshots";

const { Text } = Typography;

const PAGE_SIZE = 25;

/** How a boundary is named where its conversation is gone or unnamed. */
function conversationLabel(row: Snapshot): string {
  if (!row.conversation) return `Deleted conversation · ${shortInstanceId(row.agentInstanceId)}`;
  return conversationTitle(row.conversation);
}

/**
 * Every snapshot this cluster is holding for the reader, and the way to release them.
 *
 * ## Why the page exists
 *
 * A checkpoint is not only a row: it pins a copy of the conversation's runtime in the
 * substrate, which is what makes forking one possible and what makes it cost storage.
 * They were reachable only from inside the conversation they belong to and could not
 * be removed at all — and duplicating a chat takes one every time, so a cluster
 * accumulated snapshots nobody had asked for and nobody could find.
 *
 * ## What it does not do
 *
 * There is no create here. A snapshot is taken where it means something — at a turn
 * boundary in a conversation, from the composer's Checkpoint button — and a control
 * here would have nothing to take one *of*.
 */
export function SnapshotsPage() {
  const theme = useTheme();
  const { data, isLoading, error, isEmpty, refresh } = useSnapshots();
  const view = useListView([]);
  const [picked, setPicked] = useState<readonly string[]>([]);
  const [isDeleting, setDeleting] = useState(false);

  const snapshots = useMemo(() => data ?? [], [data]);

  const filtered = useMemo(
    () =>
      snapshots.filter((row) =>
        matchesQuery(view.query, [
          conversationLabel(row),
          row.agentInstanceId,
          row.id,
          row.headTaskId,
        ]),
      ),
    [snapshots, view.query],
  );

  /*
   * One request each, wrapped in one toast.
   *
   * The reader asked for a batch and a batch is what they should be told about — a
   * toast per snapshot for twelve of them is a column of notifications, and the count
   * is the thing they actually want back.
   */
  async function deletePicked() {
    const ids = picked;
    setDeleting(true);
    try {
      await toast.promise(
        deleteSnapshots(ids, (id) => apiClient.agentInstances.checkpoints.remove(id)),
        {
          loading: `Deleting ${ids.length} ${ids.length === 1 ? "snapshot" : "snapshots"}…`,
          success: (deleted: number) => deletedMessage(deleted),
          error: (cause: Error) => cause.message,
        },
      );
      setPicked([]);
    } catch {
      // Already reported by the toast and logged, one line per failure, by
      // `deleteSnapshots`. The rows that did go are dropped by the refresh below, and
      // the ones that did not stay picked so a second attempt needs no re-picking.
      setPicked((current) => current.filter((id) => ids.includes(id)));
    } finally {
      setDeleting(false);
      await refresh();
    }
  }

  const columns = useMemo<ColumnsType<Snapshot>>(
    () => [
      {
        title: "Conversation",
        key: "conversation",
        sorter: byText<Snapshot>(conversationLabel),
        sortOrder: sortOrderFor(view, "conversation"),
        render: (_, row) =>
          row.conversation ? (
            <Link
              to={agentUrl.chat({ id: row.agentInstanceId })}
              data-testid={`snapshot-conversation-${row.id}`}
            >
              {conversationLabel(row)}
            </Link>
          ) : (
            // Not a link: the conversation is gone, and the snapshot outliving it is
            // the reason this row is worth finding.
            <Text data-testid={`snapshot-conversation-${row.id}`} css={{ color: theme.color.textMuted }}>
              {conversationLabel(row)}
            </Text>
          ),
      },
      {
        title: "Taken",
        key: "createdAt",
        width: 200,
        sorter: byText<Snapshot>((row) => row.createdAt ?? ""),
        sortOrder: sortOrderFor(view, "createdAt"),
        render: (_, row) =>
          row.createdAt ? (
            <Text title={row.createdAt} css={{ color: theme.color.textMuted }}>
              {relativeAge(row.createdAt)}
            </Text>
          ) : (
            <Text css={{ color: theme.color.textMuted }}>Not reported</Text>
          ),
      },
      {
        title: "State",
        key: "state",
        width: 120,
        sorter: byText<Snapshot>((row) => row.state),
        sortOrder: sortOrderFor(view, "state"),
        render: (_, row) => (
          <Tag color={row.state === "ready" ? "success" : row.state === "failed" ? "error" : "default"}>
            {row.state}
          </Tag>
        ),
      },
      {
        title: "",
        key: "actions",
        width: 48,
        render: (_, row) => (
          <DeleteResourceButton
            kind="snapshot"
            name={conversationLabel(row)}
            description="The copy of the conversation's runtime this was holding is released. Chats already forked from it are unaffected."
            onDelete={() => apiClient.agentInstances.checkpoints.remove(row.id)}
            onDeleted={refresh}
          />
        ),
      },
    ],
    [refresh, theme, view],
  );

  return (
    <PageFrame
      title="Snapshots"
      description="Saved turn boundaries. Each one holds a copy of a conversation's runtime, which a fork starts from."
      actions={
        <Space size={8}>
          <RefreshButton onRefresh={refresh} what="Snapshots" loading={isLoading} />
          <Button
            danger
            icon={<Trash2 size={14} />}
            disabled={picked.length === 0}
            loading={isDeleting}
            data-testid="snapshots-delete-selected"
            onClick={() => void deletePicked()}
          >
            Delete {picked.length > 0 ? picked.length : ""}
          </Button>
        </Space>
      }
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
        {error ? (
          <Alert
            type="error"
            showIcon
            title="Could not load snapshots"
            description={error.message}
            data-testid="snapshots-error"
            action={
              <Button size="small" onClick={() => void refresh()}>
                Try again
              </Button>
            }
          />
        ) : null}

        <FilterBar
          testId="snapshots-filters"
          view={view}
          search={{
            label: "Search snapshots",
            placeholder: "Search conversations and ids",
          }}
          filters={[]}
          trailing={
            !error && !isLoading ? (
              <Text data-testid="snapshots-summary" css={{ color: theme.color.textMuted }}>
                {filtered.length} of {snapshots.length}{" "}
                {snapshots.length === 1 ? "snapshot" : "snapshots"}
              </Text>
            ) : null
          }
        />

        <Table<Snapshot>
          data-testid="snapshots-table"
          rowKey={(row) => row.id}
          columns={columns}
          dataSource={error ? [] : filtered}
          loading={isLoading}
          onChange={listTableChange<Snapshot>(view)}
          pagination={paginationFor(view, filtered.length, PAGE_SIZE)}
          rowSelection={{
            selectedRowKeys: picked as string[],
            onChange: (keys) => setPicked(keys.map(String)),
            // The header box picks and unpicks the page, which is what a reader means
            // by "all" in front of a table that is showing them twenty-five rows.
            getCheckboxProps: (row) => ({ "aria-label": `Select ${conversationLabel(row)}` }),
          }}
          locale={{
            emptyText: isEmpty
              ? "No snapshots yet. Checkpoint a chat to take one."
              : view.isNarrowed && !error
                ? "No snapshots match that search."
                : " ",
          }}
        />
      </Space>
    </PageFrame>
  );
}
