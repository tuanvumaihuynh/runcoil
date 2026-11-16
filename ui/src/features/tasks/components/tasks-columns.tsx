import { Link } from "@tanstack/react-router";

import type { components } from "@/api/schema";
import { createDataTableColumnHelper } from "@/components/data-table";
import { RunStatusBadge } from "@/features/runs/components/run-status-badge";
import { formatDateTime, formatRelativeTime } from "@/lib/format";

type TaskSummary = components["schemas"]["TaskSummary"];

const columnHelper = createDataTableColumnHelper<TaskSummary>();

export function tasksColumns(projectId: string, now: Date) {
  return [
    columnHelper.display({
      id: "task",
      header: "Task",
      cell: ({ row }) => (
        <div className="flex flex-col">
          <Link
            to="/projects/$projectId/tasks/$taskId"
            params={{ projectId, taskId: row.original.id }}
            className="w-fit font-medium hover:underline"
          >
            {row.original.name}
          </Link>
          <span className="text-xs text-muted-foreground">{row.original.app}</span>
        </div>
      ),
    }),
    columnHelper.display({
      id: "schedule",
      header: "Schedule",
      cell: ({ row }) => {
        const { schedule } = row.original;
        if (schedule === undefined) {
          return <span className="text-muted-foreground">None</span>;
        }

        return (
          <div className="flex flex-col">
            <span className="font-mono">{schedule.cron}</span>
            {schedule.pausedAt !== undefined ? (
              <span className="text-xs text-muted-foreground">Paused</span>
            ) : (
              <span
                title={formatDateTime(schedule.nextTickAt)}
                className="w-fit text-xs text-muted-foreground"
              >
                Next {formatRelativeTime(schedule.nextTickAt, now)}
              </span>
            )}
          </div>
        );
      },
    }),
    columnHelper.display({
      id: "lastRun",
      header: "Last run",
      cell: ({ row }) => {
        const { lastRun } = row.original;
        if (lastRun === undefined) {
          return <span className="text-muted-foreground">No runs yet</span>;
        }

        return (
          <div className="flex flex-col items-start gap-1">
            <Link to="/projects/$projectId/runs/$runId" params={{ projectId, runId: lastRun.id }}>
              <RunStatusBadge status={lastRun.status} />
            </Link>
            <span
              title={formatDateTime(lastRun.createdAt)}
              className="text-xs text-muted-foreground"
            >
              {formatRelativeTime(lastRun.createdAt, now)}
            </span>
          </div>
        );
      },
    }),
    columnHelper.display({
      id: "workers",
      header: "Workers",
      cell: ({ row }) =>
        row.original.liveWorkerCount === 0 ? (
          <span className="text-muted-foreground">None live</span>
        ) : (
          `${row.original.liveWorkerCount} live`
        ),
    }),
  ];
}
