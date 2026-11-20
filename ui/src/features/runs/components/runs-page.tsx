import { PageHeader } from "@/components/page-header";
import { GetStarted } from "@/features/get-started/components/get-started";
import { RunStatusFilter } from "@/features/runs/components/run-status-filter";
import { RunsTable } from "@/features/runs/components/runs-table";
import { useRuns } from "@/features/runs/queries";
import type { RunStatus } from "@/features/runs/status";
import type { Permission } from "@/lib/permissions";

type RunsPageProps = {
  projectId: string;
  permissions: readonly Permission[] | undefined;
  status: RunStatus[] | undefined;
  onStatusChange: (status: RunStatus[] | undefined) => void;
};

export function RunsPage({ projectId, permissions, status, onStatusChange }: RunsPageProps) {
  // The unfiltered list, shared with the table when no filter is set.
  const allRuns = useRuns(projectId, {});
  const noRuns = allRuns.data?.pages[0]?.runs.length === 0;

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader
        title="Runs"
        description="Track every Run, one execution of a Task, from the queue to its result."
      />

      {noRuns ? (
        <GetStarted projectId={projectId} permissions={permissions} />
      ) : (
        <>
          <div>
            <RunStatusFilter value={status} onChange={onStatusChange} />
          </div>

          <RunsTable projectId={projectId} filter={{ status }} />
        </>
      )}
    </div>
  );
}
