import type { components } from "@/api/schema";
import { createDataTableColumnHelper } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { formatDateTime, formatRelativeTime } from "@/lib/format";
import { shortId } from "@/lib/short-id";

type Worker = components["schemas"]["Worker"];
type WorkerEndReason = components["schemas"]["WorkerEndReason"];

const endReasonLabels: Record<WorkerEndReason, string> = {
  deregistered: "Deregistered",
  heartbeat_timeout: "Heartbeat timed out",
};

const columnHelper = createDataTableColumnHelper<Worker>();

function workerColumn() {
  return columnHelper.display({
    id: "worker",
    header: "Worker",
    cell: ({ row }) => (
      <div className="flex flex-col">
        <span className="font-medium">{row.original.app}</span>
        <span title={row.original.id} className="w-fit font-mono text-xs text-muted-foreground">
          {shortId(row.original.id)}
        </span>
      </div>
    ),
  });
}

function tasksColumn() {
  return columnHelper.display({
    id: "tasks",
    header: "Tasks",
    cell: ({ row }) => (
      <div className="flex flex-wrap gap-1">
        {row.original.tasks.map((task) => (
          <Badge key={task.id} variant="outline">
            {task.name}
          </Badge>
        ))}
      </div>
    ),
  });
}

function timeCell(value: string, now: Date) {
  return <span title={formatDateTime(value)}>{formatRelativeTime(value, now)}</span>;
}

export function liveWorkersColumns(now: Date) {
  return [
    workerColumn(),
    tasksColumn(),
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

export function endedWorkersColumns(now: Date) {
  return [
    workerColumn(),
    tasksColumn(),
    columnHelper.display({
      id: "ended",
      header: "Ended",
      cell: ({ row }) => row.original.endedAt && timeCell(row.original.endedAt, now),
    }),
    columnHelper.display({
      id: "reason",
      header: "Reason",
      cell: ({ row }) => row.original.endReason && endReasonLabels[row.original.endReason],
    }),
  ];
}
