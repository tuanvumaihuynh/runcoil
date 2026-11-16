import { describe, expect, it } from "vite-plus/test";

import { formatDateTime, formatRelativeTime } from "@/lib/format";

const now = new Date("2026-11-16T12:00:00Z");

function ago(seconds: number): string {
  return new Date(now.getTime() - seconds * 1000).toISOString();
}

describe("formatRelativeTime", () => {
  const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "always" });

  it("uses seconds under a minute", () => {
    expect(formatRelativeTime(ago(8), now)).toBe(relative.format(-8, "second"));
    expect(formatRelativeTime(ago(59), now)).toBe(relative.format(-59, "second"));
  });

  it("steps up to minutes, hours and days", () => {
    expect(formatRelativeTime(ago(60), now)).toBe(relative.format(-1, "minute"));
    expect(formatRelativeTime(ago(12 * 60 + 30), now)).toBe(relative.format(-12, "minute"));
    expect(formatRelativeTime(ago(3 * 3600), now)).toBe(relative.format(-3, "hour"));
    expect(formatRelativeTime(ago(2 * 86400 + 5), now)).toBe(relative.format(-2, "day"));
  });

  it("reads a time after now as the future", () => {
    expect(formatRelativeTime(ago(-30), now)).toBe(relative.format(30, "second"));
  });
});

describe("formatDateTime", () => {
  it("shows the date and the time", () => {
    const value = "2026-11-16T12:34:56Z";
    const expected = new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "medium",
    }).format(new Date(value));

    expect(formatDateTime(value)).toBe(expected);
  });
});
