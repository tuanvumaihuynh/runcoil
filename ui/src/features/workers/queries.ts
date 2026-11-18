import { client } from "@/api/client";
import { usePagedQuery } from "@/lib/paged-query";

export type WorkerState = "live" | "ended";

const liveRefetchInterval = 10_000;

export function useWorkers(projectId: string, state: WorkerState) {
  const params = { path: { projectId }, query: { state } };

  return usePagedQuery({
    path: "/projects/{projectId}/workers",
    params,
    fetchPage: ({ pageSize, pageToken, signal }) =>
      client.GET("/projects/{projectId}/workers", {
        params: { path: params.path, query: { ...params.query, pageSize, pageToken } },
        signal,
      }),
    refetchInterval: state === "live" ? liveRefetchInterval : false,
  });
}
