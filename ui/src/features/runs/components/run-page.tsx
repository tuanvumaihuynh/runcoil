import type { ReactNode } from "react";

import { JsonView, TextView } from "@/components/json-view";
import { NotFound } from "@/components/not-found";
import { PageHeader } from "@/components/page-header";
import { SettingsRow } from "@/components/settings-row";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { RunStatusBadge } from "@/features/runs/components/run-status-badge";
import { RunSteps } from "@/features/runs/components/run-steps";
import { runDuration } from "@/features/runs/duration";
import { useRun } from "@/features/runs/queries";
import { describeRunState } from "@/features/runs/run-state";
import { hasRunEnded } from "@/features/runs/status";
import { ApiError } from "@/lib/api-errors";
import { formatDateTime, formatDuration } from "@/lib/format";

type RunPageProps = {
  projectId: string;
  runId: string;
};

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="text-lg font-medium">{title}</h2>
      {children}
    </section>
  );
}

function time(value: string | undefined): string {
  return value === undefined ? "Not yet" : formatDateTime(value);
}

export function RunPage({ projectId, runId }: RunPageProps) {
  const query = useRun(projectId, runId);
  const { data: run } = query;

  if (query.isError) {
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

  const state = hasRunEnded(run.status) ? undefined : describeRunState(run);
  const duration = runDuration(run, new Date(query.dataUpdatedAt));
  const retry = run.config.retry;

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader
        title={run.task.name}
        description="Follow this run step by step."
        actions={<RunStatusBadge status={run.status} />}
      />

      {state !== undefined && (
        <Alert>
          <AlertDescription>{state}</AlertDescription>
        </Alert>
      )}

      <div className="flex flex-col">
        <SettingsRow label="Task">
          {run.task.app} / {run.task.name}
        </SettingsRow>
        <SettingsRow label="Run ID">
          <span className="font-mono break-all">{run.id}</span>
        </SettingsRow>
        <SettingsRow label="Created">{time(run.createdAt)}</SettingsRow>
        <SettingsRow label="Started">{time(run.startedAt)}</SettingsRow>
        <SettingsRow label="Ended">{time(run.endedAt)}</SettingsRow>
        <SettingsRow label="Duration">
          {duration === undefined ? "Not yet" : formatDuration(duration)}
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
