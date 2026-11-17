import { useMemo } from "react";

import { DataTable } from "@/components/data-table";
import { Button } from "@/components/ui/button";
import { useNow } from "@/hooks/use-now";
import { runsColumns } from "@/features/runs/components/runs-columns";
import { useRuns } from "@/features/runs/queries";
import type { RunFilter } from "@/features/runs/queries";

type RunsTableProps = {
  projectId: string;
  filter: RunFilter;
  /** Shows the Task column. */
  showTask?: boolean;
  /** Replaces the message of an empty table. */
  emptyMessage?: string;
};

export function RunsTable({ projectId, filter, showTask = true, emptyMessage }: RunsTableProps) {
  const runs = useRuns(projectId, filter);
  const now = useNow();

  const rows = useMemo(() => runs.data?.pages.flatMap((page) => page.runs) ?? [], [runs.data]);
  const columns = useMemo(() => runsColumns(projectId, now, showTask), [projectId, now, showTask]);
  const filtered = (filter.status?.length ?? 0) > 0 || filter.taskId !== undefined;

  return (
    <div className="flex flex-col gap-6">
      {runs.isError && runs.data === undefined ? (
        <div className="flex flex-col items-start gap-3">
          <p role="alert" className="text-sm text-destructive">
            Could not load runs.
          </p>
          <Button variant="outline" onClick={() => void runs.refetch()}>
            Try again
          </Button>
        </div>
      ) : (
        <DataTable
          columns={columns}
          data={rows}
          loading={runs.isPending}
          emptyMessage={emptyMessage ?? (filtered ? "No runs match this filter." : "No runs yet.")}
        />
      )}

      {runs.hasNextPage && (
        <div>
          <Button
            variant="outline"
            onClick={() => void runs.fetchNextPage()}
            disabled={runs.isFetchingNextPage}
          >
            Load more
          </Button>
        </div>
      )}
    </div>
  );
}
