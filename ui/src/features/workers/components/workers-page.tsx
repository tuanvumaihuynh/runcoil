import { Link } from "@tanstack/react-router";
import { useMemo } from "react";

import { DataTable } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  endedWorkersColumns,
  liveWorkersColumns,
} from "@/features/workers/components/workers-columns";
import { useRuns } from "@/features/runs/queries";
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
  const noneLive = state === "live" && workers.data?.pages[0]?.workers.length === 0;
  // A project with no Run still shows the Get started steps on the Runs page.
  const runs = useRuns(projectId, {}, { enabled: noneLive });
  // Only a list that loaded empty sends the user to Get started; while it loads, or if it fails,
  // the ended Workers are the safer next step.
  const noRuns = runs.isSuccess && runs.data.pages[0]?.runs.length === 0;

  const columns = useMemo(
    () =>
      state === "live" ? liveWorkersColumns(projectId, now) : endedWorkersColumns(projectId, now),
    [projectId, state, now],
  );

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader
        title="Workers"
        description="Monitor your Workers, the processes that connect to Runcoil and run your Tasks."
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
          emptyMessage={
            state === "live" ? (
              <>
                A Worker is a process of yours that connects to Runcoil and runs your Tasks. None is
                connected, so no Run can start.{" "}
                {noRuns ? (
                  <Button asChild variant="link" className="h-auto p-0">
                    <Link to="/projects/$projectId/runs" params={{ projectId }}>
                      Get started
                    </Link>
                  </Button>
                ) : (
                  <Button
                    variant="link"
                    className="h-auto p-0"
                    onClick={() => onStateChange("ended")}
                  >
                    See ended Workers
                  </Button>
                )}
              </>
            ) : (
              "No Worker has ended yet."
            )
          }
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
