type Timespan = { startedAt?: string; endedAt?: string };

// Milliseconds a Run has run, up to now while it has not ended. Undefined before it starts.
export function runDuration({ startedAt, endedAt }: Timespan, now: Date): number | undefined {
  if (startedAt === undefined) {
    return undefined;
  }

  return (
    (endedAt === undefined ? now.getTime() : new Date(endedAt).getTime()) -
    new Date(startedAt).getTime()
  );
}
