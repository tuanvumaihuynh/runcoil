import { client } from "@/api/client";
import { usePagedQuery } from "@/lib/paged-query";

export type WorkerState = "live" | "ended";

const liveRefetchInterval = 10_000;

export function useWorkers(projectId: string, state: WorkerState) {
  return usePagedQuery({
    path: "/projects/{projectId}/workers",
    params: { path: { projectId }, query: { state } },
    fetchPage: ({ pageSize, pageToken, signal }) =>
      client.GET("/projects/{projectId}/workers", {
        params: { path: { projectId }, query: { state, pageSize, pageToken } },
        signal,
      }),
    refetchInterval: state === "live" ? liveRefetchInterval : false,
  });
}
