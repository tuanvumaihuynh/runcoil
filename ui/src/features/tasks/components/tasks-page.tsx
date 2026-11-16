import { useMemo } from "react";

import { DataTable } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { tasksColumns } from "@/features/tasks/components/tasks-columns";
import { useTasks } from "@/features/tasks/queries";

type TasksPageProps = {
  projectId: string;
};

export function TasksPage({ projectId }: TasksPageProps) {
  const tasks = useTasks(projectId);
  const { dataUpdatedAt } = tasks;

  const rows = useMemo(() => tasks.data?.pages.flatMap((page) => page.tasks) ?? [], [tasks.data]);
  const columns = useMemo(
    () => tasksColumns(projectId, new Date(dataUpdatedAt)),
    [projectId, dataUpdatedAt],
  );

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader title="Tasks" description="Browse the tasks your workers have registered." />

      {tasks.isError ? (
        <div className="flex flex-col items-start gap-3">
          <p role="alert" className="text-sm text-destructive">
            Could not load tasks.
          </p>
          <Button variant="outline" onClick={() => void tasks.refetch()}>
            Try again
          </Button>
        </div>
      ) : (
        <DataTable
          columns={columns}
          data={rows}
          loading={tasks.isPending}
          emptyMessage="No tasks yet."
        />
      )}

      {tasks.hasNextPage && (
        <div>
          <Button
            variant="outline"
            onClick={() => void tasks.fetchNextPage()}
            disabled={tasks.isFetchingNextPage}
          >
            Load more
          </Button>
        </div>
      )}
    </div>
  );
}
