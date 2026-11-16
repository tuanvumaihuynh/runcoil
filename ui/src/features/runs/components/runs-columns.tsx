import { Link } from "@tanstack/react-router";

import type { components } from "@/api/schema";
import { createDataTableColumnHelper } from "@/components/data-table";
import { RunStatusBadge } from "@/features/runs/components/run-status-badge";
import { runDuration } from "@/features/runs/duration";
import { formatDateTime, formatDuration, formatRelativeTime } from "@/lib/format";
import { shortId } from "@/lib/short-id";

type RunSummary = components["schemas"]["RunSummary"];

const columnHelper = createDataTableColumnHelper<RunSummary>();

export function runsColumns(projectId: string, now: Date) {
  return [
    columnHelper.display({
      id: "status",
      header: "Status",
      cell: ({ row }) => <RunStatusBadge status={row.original.status} />,
    }),
    columnHelper.display({
      id: "task",
      header: "Task",
      cell: ({ row }) => (
        <div className="flex flex-col">
          <span className="font-medium">{row.original.task.name}</span>
          <span className="text-xs text-muted-foreground">{row.original.task.app}</span>
        </div>
      ),
    }),
    columnHelper.display({
      id: "run",
      header: "Run",
      cell: ({ row }) => (
        <Link
          to="/projects/$projectId/runs/$runId"
          params={{ projectId, runId: row.original.id }}
          title={row.original.id}
          className="font-mono text-xs hover:underline"
        >
          {shortId(row.original.id)}
        </Link>
      ),
    }),
    columnHelper.display({
      id: "created",
      header: "Created",
      cell: ({ row }) => (
        <span title={formatDateTime(row.original.createdAt)}>
          {formatRelativeTime(row.original.createdAt, now)}
        </span>
      ),
    }),
    columnHelper.display({
      id: "duration",
      header: "Duration",
      cell: ({ row }) => {
        const duration = runDuration(row.original, now);
        return duration === undefined ? null : formatDuration(duration);
      },
    }),
  ];
}
