import type { Key } from "react";
import { Skeleton, Table, type TableProps } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useTheme } from "@emotion/react";

/** Placeholder rows in the real table shell, sharing its columns, padding and radius. */
export function TableSkeleton<T extends object>({
  columns,
  testId,
  label,
  doubleLineColumn,
  scroll,
}: {
  columns: ColumnsType<T>;
  testId: string;
  label: string;
  doubleLineColumn?: Key;
  scroll?: TableProps<T>["scroll"];
}) {
  const theme = useTheme();
  const bar = (key: Key | undefined) => (
    <Skeleton.Button
      active
      styles={{ root: {
        display: "flex",
        alignItems: "center",
        width: "100%",
        height: key === "actions" ? 32 : 22,
      } }}
      style={{
        width: key === "actions" ? 32 : "70%",
        maxWidth: 180,
        height: key === "actions" ? 32 : 14,
        borderRadius: theme.radius.sm,
      }}
    />
  );

  return (
    <div role="status" aria-label={label} aria-busy="true" data-testid={testId}>
      <div aria-hidden="true">
        <Table<{ id: number }>
          rowKey="id"
          tableLayout="fixed"
          columns={columns.map((column) => ({
            key: column.key,
            title: typeof column.title === "function" ? column.title({}) : column.title,
            width: column.width,
            render: () => column.key === doubleLineColumn ? (
              <div css={{ display: "grid", gridAutoRows: "22px", minHeight: 44 }}>
                {bar(column.key)}
                {bar(column.key)}
              </div>
            ) : bar(column.key),
          }))}
          dataSource={[{ id: 0 }, { id: 1 }, { id: 2 }]}
          pagination={false}
          scroll={scroll}
        />
      </div>
    </div>
  );
}
