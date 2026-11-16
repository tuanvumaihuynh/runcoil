import { useMemo } from "react";

import type { components } from "@/api/schema";
import { createDataTableColumnHelper, DataTable } from "@/components/data-table";
import { formatDateTime, formatRelativeTime } from "@/lib/format";
import { shortId } from "@/lib/short-id";

type TaskWorker = components["schemas"]["Task"]["workers"][number];

const columnHelper = createDataTableColumnHelper<TaskWorker>();

function timeCell(value: string, now: Date) {
  return <span title={formatDateTime(value)}>{formatRelativeTime(value, now)}</span>;
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
  updatedAt: number;
};

export function TaskWorkersTable({ workers, updatedAt }: TaskWorkersTableProps) {
  const columns = useMemo(() => taskWorkersColumns(new Date(updatedAt)), [updatedAt]);

  return (
    <DataTable columns={columns} data={workers} emptyMessage="No live workers serve this task." />
  );
}
