import { useMemo } from "react";

import { DataTable } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  endedWorkersColumns,
  liveWorkersColumns,
} from "@/features/workers/components/workers-columns";
import { useNow } from "@/hooks/use-now";
import { useWorkers } from "@/features/workers/queries";
import type { WorkerState } from "@/features/workers/queries";

type WorkersPageProps = {
  projectId: string;
  state: WorkerState;
  onStateChange: (state: WorkerState) => void;
};

export function WorkersPage({ projectId, state, onStateChange }: WorkersPageProps) {
  const workers = useWorkers(projectId, state);
  const now = useNow();

  const rows = useMemo(
    () => workers.data?.pages.flatMap((page) => page.workers) ?? [],
    [workers.data],
  );
  const columns = useMemo(
    () =>
      state === "live" ? liveWorkersColumns(projectId, now) : endedWorkersColumns(projectId, now),
    [projectId, state, now],
  );

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader
        title="Workers"
        description="Monitor the workers connected to your project."
        actions={
          <ToggleGroup
            type="single"
            variant="outline"
            size="sm"
            aria-label="Worker state"
            value={state}
            onValueChange={(value) => {
              if (value === "live" || value === "ended") {
                onStateChange(value);
              }
            }}
          >
            <ToggleGroupItem value="live">Live</ToggleGroupItem>
            <ToggleGroupItem value="ended">Ended</ToggleGroupItem>
          </ToggleGroup>
        }
      />

      {workers.isError && workers.data === undefined ? (
        <div className="flex flex-col items-start gap-3">
          <p role="alert" className="text-sm text-destructive">
            Could not load workers.
          </p>
          <Button variant="outline" onClick={() => void workers.refetch()}>
            Try again
          </Button>
        </div>
      ) : (
        <DataTable
          columns={columns}
          data={rows}
          loading={workers.isPending}
          emptyMessage={state === "live" ? "No workers are connected." : "No workers have ended."}
        />
      )}

      {workers.hasNextPage && (
        <div>
          <Button
            variant="outline"
            onClick={() => void workers.fetchNextPage()}
            disabled={workers.isFetchingNextPage}
          >
            Load more
          </Button>
        </div>
      )}
    </div>
  );
}
