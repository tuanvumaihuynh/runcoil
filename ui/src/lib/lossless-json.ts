type RawJson = (text: string) => unknown;

type JsonWithRaw = typeof JSON & { rawJSON?: RawJson };

type ReviverContext = { source?: string };

// JSON.parse turns 12345678901234567890 into a double, and stringify prints it back as 12345678901234567000.
// A number whose digits would change is kept as raw JSON so stringify prints the original digits.
export function parseJson(text: string): unknown {
  const rawJSON = (JSON as JsonWithRaw).rawJSON;
  if (rawJSON === undefined) {
    return JSON.parse(text);
  }

  return JSON.parse(text, function (_key, value: unknown, context?: ReviverContext) {
    if (typeof value === "number" && context?.source !== undefined) {
      return context.source === String(value) ? value : rawJSON(context.source);
    }

    return value;
  });
}
