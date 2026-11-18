const dateFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });

export function formatDate(value: string): string {
  return dateFormat.format(new Date(value));
}

const dateTimeFormat = new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "medium",
});

export function formatDateTime(value: string): string {
  return dateTimeFormat.format(new Date(value));
}

const relativeTimeFormat = new Intl.RelativeTimeFormat(undefined, { numeric: "always" });

const relativeUnits: { unit: Intl.RelativeTimeFormatUnit; seconds: number }[] = [
  { unit: "day", seconds: 86400 },
  { unit: "hour", seconds: 3600 },
  { unit: "minute", seconds: 60 },
];

export function formatRelativeTime(value: string, now: Date): string {
  const elapsed = Math.round((new Date(value).getTime() - now.getTime()) / 1000);
  const magnitude = Math.abs(elapsed);

  for (const { unit, seconds } of relativeUnits) {
    if (magnitude >= seconds) {
      return relativeTimeFormat.format(Math.trunc(elapsed / seconds), unit);
    }
  }

  return relativeTimeFormat.format(elapsed, "second");
}

// Short forms such as 850ms, 4.1s, 2m 3s and 1h 5m. Intl.DurationFormat has no TypeScript lib types yet and spells the units out.
export function formatDuration(ms: number): string {
  const milliseconds = Math.max(0, Math.round(ms));
  if (milliseconds < 1000) {
    return `${milliseconds}ms`;
  }

  const tenths = Math.round(milliseconds / 100);
  if (tenths < 600) {
    return `${tenths / 10}s`;
  }

  const totalSeconds = Math.round(milliseconds / 1000);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;

  if (hours > 0) {
    return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`;
  }

  return seconds > 0 ? `${minutes}m ${seconds}s` : `${minutes}m`;
}
