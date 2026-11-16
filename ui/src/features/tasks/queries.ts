import { api, client } from "@/api/client";
import { usePagedQuery } from "@/lib/paged-query";

const refetchInterval = 10_000;

export function useTasks(projectId: string) {
  return usePagedQuery({
    path: "/projects/{projectId}/tasks",
    params: { path: { projectId } },
    fetchPage: ({ pageSize, pageToken, signal }) =>
      client.GET("/projects/{projectId}/tasks", {
        params: { path: { projectId }, query: { pageSize, pageToken } },
        signal,
      }),
    refetchInterval,
  });
}

export function useTask(projectId: string, taskId: string) {
  return api.useQuery(
    "get",
    "/projects/{projectId}/tasks/{taskId}",
    { params: { path: { projectId, taskId } } },
    { refetchInterval: ({ state }) => (state.status === "error" ? false : refetchInterval) },
  );
}
