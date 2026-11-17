import { formatDateTime, formatRelativeTime } from "@/lib/format";

type RelativeTimeProps = {
  value: string;
  now: Date;
  className?: string;
};

export function RelativeTime({ value, now, className }: RelativeTimeProps) {
  return (
    <time dateTime={value} title={formatDateTime(value)} className={className}>
      {formatRelativeTime(value, now)}
    </time>
  );
}
