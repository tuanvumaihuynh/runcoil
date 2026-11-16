import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";

import { api, client } from "@/api/client";

const lookupPageSize = 50;

export function useMembers(projectId: string) {
  return api.useQuery("get", "/projects/{projectId}/members", {
    params: { path: { projectId } },
  });
}

export function useUserLookup(enabled: boolean) {
  // Not api.useInfiniteQuery: it sends pageToken=0 on the first page, which the server rejects.
  return useInfiniteQuery({
    queryKey: ["get", "/users/lookup", { params: { query: { pageSize: lookupPageSize } } }],
    queryFn: async ({ pageParam, signal }) => {
      const { data, error } = await client.GET("/users/lookup", {
        params: { query: { pageSize: lookupPageSize, pageToken: pageParam } },
        signal,
      });
      if (error) {
        throw error;
      }
      return data;
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextPageToken,
    enabled,
  });
}

function invalidateMembers(queryClient: ReturnType<typeof useQueryClient>) {
  return queryClient.invalidateQueries({ queryKey: ["get", "/projects/{projectId}/members"] });
}

export function useAddMember() {
  const queryClient = useQueryClient();

  return api.useMutation("post", "/projects/{projectId}/members", {
    onSuccess: () => invalidateMembers(queryClient),
  });
}

export function useUpdateMember() {
  const queryClient = useQueryClient();

  return api.useMutation("put", "/projects/{projectId}/members/{userId}", {
    onSuccess: () => Promise.all([invalidateMembers(queryClient), invalidateMe(queryClient)]),
  });
}

export function useRemoveMember() {
  const queryClient = useQueryClient();

  return api.useMutation("delete", "/projects/{projectId}/members/{userId}", {
    onSuccess: () => Promise.all([invalidateMembers(queryClient), invalidateMe(queryClient)]),
  });
}

function invalidateMe(queryClient: ReturnType<typeof useQueryClient>) {
  return queryClient.invalidateQueries({ queryKey: ["get", "/me"] });
}
