import { describe, expect, it } from "vite-plus/test";

import { formatDateTime, formatDuration, formatRelativeTime } from "@/lib/format";

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

describe("formatDuration", () => {
  it("uses milliseconds under a second", () => {
    expect(formatDuration(0)).toBe("0ms");
    expect(formatDuration(850)).toBe("850ms");
    expect(formatDuration(999.4)).toBe("999ms");
  });

  it("uses seconds with one decimal under a minute", () => {
    expect(formatDuration(1000)).toBe("1s");
    expect(formatDuration(4100)).toBe("4.1s");
    expect(formatDuration(12_340)).toBe("12.3s");
    expect(formatDuration(59_940)).toBe("59.9s");
  });

  it("uses minutes and seconds under an hour", () => {
    expect(formatDuration(123_000)).toBe("2m 3s");
    expect(formatDuration(120_000)).toBe("2m");
    expect(formatDuration(59_960)).toBe("1m");
  });

  it("uses hours and minutes from an hour", () => {
    expect(formatDuration(3_900_000)).toBe("1h 5m");
    expect(formatDuration(7_200_000)).toBe("2h");
    expect(formatDuration(90_000_000)).toBe("25h");
  });

  it("reads a negative span as zero", () => {
    expect(formatDuration(-5)).toBe("0ms");
  });
});
