import { Link } from "@tanstack/react-router";
import { useMemo } from "react";

import { DataTable } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { useNow } from "@/hooks/use-now";
import { tasksColumns } from "@/features/tasks/components/tasks-columns";
import { useTasks } from "@/features/tasks/queries";

type TasksPageProps = {
  projectId: string;
};

export function TasksPage({ projectId }: TasksPageProps) {
  const tasks = useTasks(projectId);
  const now = useNow();

  const rows = useMemo(() => tasks.data?.pages.flatMap((page) => page.tasks) ?? [], [tasks.data]);
  const columns = useMemo(() => tasksColumns(projectId, now), [projectId, now]);

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader
        title="Tasks"
        description="Browse your Tasks, the functions your Workers register and Runcoil runs."
      />

      {tasks.isError && tasks.data === undefined ? (
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
          emptyMessage={
            <>
              A Task is a function your Worker registers with Runcoil. Start a Worker and its Tasks
              appear here.{" "}
              <Link
                to="/projects/$projectId/runs"
                params={{ projectId }}
                className="text-foreground underline underline-offset-4"
              >
                Get started
              </Link>
            </>
          }
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
