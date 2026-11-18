import { describe, expect, it } from "vite-plus/test";

import { workerCommands } from "@/features/get-started/worker-snippet";

describe("workerCommands", () => {
  it("points the Worker at port 7070 over http on the dashboard's host", () => {
    expect(workerCommands({ hostname: "runcoil.example.com" }).split("\n").at(-1)).toBe(
      'RUNCOIL_URL=http://runcoil.example.com:7070 RUNCOIL_API_KEY="YOUR_API_KEY" go run .',
    );
  });

  it("sets up the module before it runs", () => {
    expect(workerCommands({ hostname: "localhost" }).split("\n").slice(0, 2)).toEqual([
      "go mod init hello",
      "go mod tidy",
    ]);
  });
});
