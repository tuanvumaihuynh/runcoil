import type { components } from "@/api/schema";
import { NotFound } from "@/components/not-found";
import { Section } from "@/components/page-section";
import { PageHeader } from "@/components/page-header";
import { SettingsRow } from "@/components/settings-row";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { RunsTable } from "@/features/runs/components/runs-table";
import { TaskWorkersTable } from "@/features/tasks/components/task-workers-table";
import { useTask } from "@/features/tasks/queries";
import { ApiError } from "@/lib/api-errors";
import { formatDateTime } from "@/lib/format";

type TaskSchedule = components["schemas"]["TaskSchedule"];

type TaskPageProps = {
  projectId: string;
  taskId: string;
};

const overlapLabels: Record<TaskSchedule["overlap"], string> = {
  skip: "Skip a tick while a run is in progress",
  allow: "Start a run on every tick",
};

function ScheduleRows({ schedule }: { schedule: TaskSchedule }) {
  const paused = schedule.pausedAt !== undefined;

  return (
    <div className="flex flex-col">
      <SettingsRow label="Cron" description="Read in UTC.">
        <span className="font-mono">{schedule.cron}</span>
      </SettingsRow>
      <SettingsRow label="Next run">
        {paused ? "Paused" : formatDateTime(schedule.nextTickAt)}
      </SettingsRow>
      <SettingsRow label="Overlap">{overlapLabels[schedule.overlap]}</SettingsRow>
      <SettingsRow label="State">
        {schedule.pausedAt === undefined
          ? "Active"
          : `Paused since ${formatDateTime(schedule.pausedAt)}`}
      </SettingsRow>
    </div>
  );
}

export function TaskPage({ projectId, taskId }: TaskPageProps) {
  const query = useTask(projectId, taskId);
  const { data: task } = query;

  if (query.isError) {
    if (query.error instanceof ApiError && query.error.status === 404) {
      return <NotFound />;
    }

    return (
      <div className="flex flex-col items-start gap-3">
        <p role="alert" className="text-sm text-destructive">
          Could not load this task.
        </p>
        <Button variant="outline" onClick={() => void query.refetch()}>
          Try again
        </Button>
      </div>
    );
  }

  if (task === undefined) {
    return (
      <div className="flex w-full flex-col gap-6">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-24 w-full" />
      </div>
    );
  }

  const { retry } = task;

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader title={task.name} description="See this task's schedule, workers, and runs." />

      <div className="flex flex-col">
        <SettingsRow label="App">{task.app}</SettingsRow>
        <SettingsRow label="Task ID">
          <span className="font-mono break-all">{task.id}</span>
        </SettingsRow>
        <SettingsRow label="First registered">{formatDateTime(task.createdAt)}</SettingsRow>
        <SettingsRow label="Last registered">{formatDateTime(task.lastRegisteredAt)}</SettingsRow>
      </div>

      <Section title="Schedule">
        {task.schedule === undefined ? (
          <p className="text-sm text-muted-foreground">This task has no schedule.</p>
        ) : (
          <ScheduleRows schedule={task.schedule} />
        )}
      </Section>

      <Section title="Settings">
        <div className="flex flex-col">
          <SettingsRow label="Max attempts">{retry?.maxAttempts ?? "Server default"}</SettingsRow>
          <SettingsRow label="Delays">
            {retry?.delays === undefined ? "Server default" : retry.delays.join(", ")}
          </SettingsRow>
          <SettingsRow label="Start deadline">{task.startDeadline ?? "None"}</SettingsRow>
        </div>
      </Section>

      <Section title="Workers">
        <TaskWorkersTable workers={task.workers} updatedAt={query.dataUpdatedAt} />
      </Section>

      <Section title="Runs">
        <RunsTable projectId={projectId} filter={{ taskId }} />
      </Section>
    </div>
  );
}
