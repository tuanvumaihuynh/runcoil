import { useMemo } from "react";

import type { components } from "@/api/schema";
import { createDataTableColumnHelper, DataTable } from "@/components/data-table";
import { RelativeTime } from "@/components/relative-time";
import { useNow } from "@/hooks/use-now";
import { shortId } from "@/lib/short-id";

type TaskWorker = components["schemas"]["Task"]["workers"][number];

const columnHelper = createDataTableColumnHelper<TaskWorker>();

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
      cell: ({ row }) => <RelativeTime value={row.original.connectedAt} now={now} />,
    }),
    columnHelper.display({
      id: "heartbeat",
      header: "Last heartbeat",
      cell: ({ row }) => <RelativeTime value={row.original.heartbeatAt} now={now} />,
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
