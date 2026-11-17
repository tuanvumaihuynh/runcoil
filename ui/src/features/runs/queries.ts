import { useQuery } from "@tanstack/react-query";

import type { components } from "@/api/schema";
import { api, client } from "@/api/client";
import { hasRunEnded } from "@/features/runs/status";
import type { RunStatus } from "@/features/runs/status";
import { parseJson } from "@/lib/lossless-json";
import { usePagedQuery } from "@/lib/paged-query";

export type RunFilter = { status?: RunStatus[]; taskId?: string };

const listRefetchInterval = 5_000;
const runRefetchInterval = 2_000;

export function useRuns(projectId: string, filter: RunFilter) {
  const query = { status: filter.status, taskId: filter.taskId };

  return usePagedQuery({
    path: "/projects/{projectId}/runs",
    params: { path: { projectId }, query },
    fetchPage: ({ pageSize, pageToken, signal }) =>
      client.GET("/projects/{projectId}/runs", {
        params: { path: { projectId }, query: { ...query, pageSize, pageToken } },
        signal,
      }),
    refetchInterval: listRefetchInterval,
  });
}

// The body is read as text so that large numbers in input and output keep their digits.
export function useRun(projectId: string, runId: string) {
  const init = { params: { path: { projectId, runId } } };

  return useQuery({
    queryKey: api.queryOptions("get", "/projects/{projectId}/runs/{runId}", init).queryKey,
    queryFn: async ({ signal }) => {
      const { data, error } = await client.GET("/projects/{projectId}/runs/{runId}", {
        ...init,
        parseAs: "text",
        signal,
      });
      if (error || data === undefined) {
        throw error ?? new Error("The server returned no data for the run");
      }

      return parseJson(data) as components["schemas"]["Run"];
    },
    refetchInterval: ({ state }) =>
      state.data === undefined || hasRunEnded(state.data.status) ? false : runRefetchInterval,
  });
}
