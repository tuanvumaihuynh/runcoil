import { formatDateTime, formatRelativeTime, formatTimeAgo } from "@/lib/format";

type RelativeTimeProps = {
  value: string;
  now: Date;
  className?: string;
  /** Set for a time that has already passed, so one newer than `now` reads "just now". */
  past?: boolean;
};

export function RelativeTime({ value, now, className, past = false }: RelativeTimeProps) {
  return (
    <time dateTime={value} title={formatDateTime(value)} className={className}>
      {past ? formatTimeAgo(value, now) : formatRelativeTime(value, now)}
    </time>
  );
}
