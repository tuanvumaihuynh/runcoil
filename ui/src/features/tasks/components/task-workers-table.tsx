import { useMemo } from "react";

import type { components } from "@/api/schema";
import { createDataTableColumnHelper, DataTable } from "@/components/data-table";
import { formatDateTime, formatRelativeTime } from "@/lib/format";
import { useNow } from "@/hooks/use-now";
import { shortId } from "@/lib/short-id";

type TaskWorker = components["schemas"]["Task"]["workers"][number];

const columnHelper = createDataTableColumnHelper<TaskWorker>();

function timeCell(value: string, now: Date) {
  return (
    <time dateTime={value} title={formatDateTime(value)}>
      {formatRelativeTime(value, now)}
    </time>
  );
}

function taskWorkersColumns(now: Date) {
  return [
    columnHelper.display({
      id: "worker",
      header: "Worker",
      cell: ({ row }) => (
        <span title={row.original.id} className="font-mono text-xs">
          {shortId(row.original.id)}
        </span>
      ),
    }),
    columnHelper.accessor("slots", { header: "Slots" }),
    columnHelper.display({
      id: "connected",
      header: "Connected",
      cell: ({ row }) => timeCell(row.original.connectedAt, now),
    }),
    columnHelper.display({
      id: "heartbeat",
      header: "Last heartbeat",
      cell: ({ row }) => timeCell(row.original.heartbeatAt, now),
    }),
  ];
}

type TaskWorkersTableProps = {
  workers: TaskWorker[];
};

export function TaskWorkersTable({ workers }: TaskWorkersTableProps) {
  const now = useNow();
  const columns = useMemo(() => taskWorkersColumns(now), [now]);

  return (
    <DataTable columns={columns} data={workers} emptyMessage="No live workers serve this task." />
  );
}
