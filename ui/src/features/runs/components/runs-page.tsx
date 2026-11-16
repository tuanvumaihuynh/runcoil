import { PageHeader } from "@/components/page-header";
import { RunStatusFilter } from "@/features/runs/components/run-status-filter";
import { RunsTable } from "@/features/runs/components/runs-table";
import type { RunStatus } from "@/features/runs/status";

type RunsPageProps = {
  projectId: string;
  status: RunStatus[] | undefined;
  onStatusChange: (status: RunStatus[] | undefined) => void;
};

export function RunsPage({ projectId, status, onStatusChange }: RunsPageProps) {
  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader title="Runs" description="Track task runs and inspect their results." />

      <div>
        <RunStatusFilter value={status} onChange={onStatusChange} />
      </div>

      <RunsTable projectId={projectId} filter={{ status }} />
    </div>
  );
}
