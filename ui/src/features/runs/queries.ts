import { api, client } from "@/api/client";
import { hasRunEnded } from "@/features/runs/status";
import type { RunStatus } from "@/features/runs/status";
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

export function useRun(projectId: string, runId: string) {
  return api.useQuery(
    "get",
    "/projects/{projectId}/runs/{runId}",
    { params: { path: { projectId, runId } } },
    {
      refetchInterval: ({ state }) =>
        state.data === undefined || hasRunEnded(state.data.status) ? false : runRefetchInterval,
    },
  );
}
