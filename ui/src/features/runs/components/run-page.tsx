import { Link } from "@tanstack/react-router";

import { JsonView, TextView } from "@/components/json-view";
import { NotFound } from "@/components/not-found";
import { PageHeader } from "@/components/page-header";
import { Section } from "@/components/page-section";
import { SettingsRow } from "@/components/settings-row";
import { Alert, AlertAction, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { RunLifecycle } from "@/features/runs/components/run-lifecycle";
import { RunStatusBadge } from "@/features/runs/components/run-status-badge";
import { RunSteps } from "@/features/runs/components/run-steps";
import { runDuration } from "@/features/runs/duration";
import { runLifecycle } from "@/features/runs/lifecycle";
import { useRun } from "@/features/runs/queries";
import { useNow } from "@/hooks/use-now";
import { ApiError } from "@/lib/api-errors";
import { formatDateTime, formatDuration } from "@/lib/format";

type RunPageProps = {
  projectId: string;
  runId: string;
};

function time(value: string | undefined, absent: string): string {
  return value === undefined ? absent : formatDateTime(value);
}

export function RunPage({ projectId, runId }: RunPageProps) {
  const query = useRun(projectId, runId);
  const { data: run } = query;
  // Every second while a retry counts down, so the countdown reads as one.
  const now = useNow(run?.waiting?.reason === "retry" ? 1_000 : 10_000);

  if (query.isError && run === undefined) {
    if (query.error instanceof ApiError && query.error.status === 404) {
      return <NotFound />;
    }

    return (
      <div className="flex flex-col items-start gap-3">
        <p role="alert" className="text-sm text-destructive">
          Could not load this run.
        </p>
        <Button variant="outline" onClick={() => void query.refetch()}>
          Try again
        </Button>
      </div>
    );
  }

  if (run === undefined) {
    return (
      <div className="flex w-full flex-col gap-6">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-24 w-full" />
      </div>
    );
  }

  const duration = runDuration(run, now);
  const retry = run.config.retry;
  const ended = run.endedAt !== undefined;

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader
        title={run.task.name}
        description="Follow this Run as it records each Step; a retry resumes after the last saved Step."
        actions={<RunStatusBadge status={run.status} />}
      />

      {query.isError && (
        <Alert variant="destructive">
          <AlertDescription>
            Could not refresh this run. What you see may be out of date.
          </AlertDescription>
          <AlertAction>
            <Button variant="outline" size="sm" onClick={() => void query.refetch()}>
              Try again
            </Button>
          </AlertAction>
        </Alert>
      )}

      <Section title="Lifecycle">
        <RunLifecycle entries={runLifecycle(run, now)} />
      </Section>

      <div className="flex flex-col">
        <SettingsRow label="Task">
          <Link
            to="/projects/$projectId/tasks/$taskId"
            params={{ projectId, taskId: run.task.id }}
            className="hover:underline"
          >
            {run.task.app} / {run.task.name}
          </Link>
        </SettingsRow>
        <SettingsRow label="Run ID">
          <span className="font-mono break-all">{run.id}</span>
        </SettingsRow>
        <SettingsRow label="Created">{time(run.createdAt, "Not yet")}</SettingsRow>
        <SettingsRow label="Started">
          {time(run.startedAt, ended ? "Never" : "Not yet")}
        </SettingsRow>
        <SettingsRow label="Ended">{time(run.endedAt, "Not yet")}</SettingsRow>
        <SettingsRow label="Duration">
          {duration === undefined ? (ended ? "None" : "Not yet") : formatDuration(duration)}
        </SettingsRow>
      </div>

      <Section title="Steps">
        <RunSteps steps={run.steps} since={run.startedAt ?? run.createdAt} />
      </Section>

      <Section title="Input">
        {run.input === undefined ? (
          <p className="text-sm text-muted-foreground">No input.</p>
        ) : (
          <JsonView value={run.input} />
        )}
      </Section>

      {run.output !== undefined && (
        <Section title="Output">
          <JsonView value={run.output} />
        </Section>
      )}

      {run.error !== undefined && (
        <Section title="Error">
          <TextView text={`${run.error.code}: ${run.error.message}`} />
        </Section>
      )}

      <Section title="Retry policy">
        <div className="flex flex-col">
          <SettingsRow label="Max attempts">{retry?.maxAttempts ?? "Server default"}</SettingsRow>
          <SettingsRow label="Delays">
            {retry?.delays === undefined ? "Server default" : retry.delays.join(", ")}
          </SettingsRow>
        </div>
      </Section>
    </div>
  );
}
