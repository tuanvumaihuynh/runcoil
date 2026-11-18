import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import { parseJson } from "@/lib/lossless-json";

function roundTrip(text: string): string {
  return JSON.stringify(parseJson(text), null, 2);
}

describe("parseJson", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("keeps the digits of a big integer", () => {
    expect(roundTrip('{"orderId":12345678901234567890}')).toBe(
      '{\n  "orderId": 12345678901234567890\n}',
    );
  });

  it("keeps the digits of a long decimal", () => {
    expect(roundTrip("0.10000000000000000001")).toBe("0.10000000000000000001");
  });

  it("leaves small numbers as numbers", () => {
    const value = parseJson('{"a":1,"b":-2.5,"c":0}');
    expect(value).toEqual({ a: 1, b: -2.5, c: 0 });
  });

  it("reaches numbers inside objects and arrays", () => {
    const text = '{"items":[{"id":12345678901234567890,"n":3}],"ok":true,"name":"x","none":null}';
    expect(JSON.stringify(parseJson(text))).toBe(text);
  });

  it("parses like JSON.parse when rawJSON is missing", () => {
    vi.stubGlobal("JSON", { parse: JSON.parse, stringify: JSON.stringify });
    const value = parseJson('{"orderId":12345678901234567890,"n":1}') as { orderId: unknown };
    expect(value.orderId).toBe(Number("12345678901234567890"));
    expect(JSON.stringify(value)).toBe('{"orderId":12345678901234567000,"n":1}');
  });
});
