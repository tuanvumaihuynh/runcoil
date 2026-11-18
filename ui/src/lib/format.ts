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
