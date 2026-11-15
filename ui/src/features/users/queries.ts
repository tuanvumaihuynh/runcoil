import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";

import { api, client } from "@/api/client";

const usersPageSize = 50;

export function useUsers() {
  // Not api.useInfiniteQuery: it sends pageToken=0 on the first page, which the server rejects.
  return useInfiniteQuery({
    queryKey: ["get", "/users", { params: { query: { pageSize: usersPageSize } } }],
    queryFn: async ({ pageParam, signal }) => {
      const { data, error } = await client.GET("/users", {
        params: { query: { pageSize: usersPageSize, pageToken: pageParam } },
        signal,
      });
      if (error) {
        throw error;
      }
      return data;
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextPageToken,
  });
}

export function useCreateUser() {
  const queryClient = useQueryClient();

  return api.useMutation("post", "/users", {
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["get", "/users"] }),
  });
}

export function useUpdateUser() {
  const queryClient = useQueryClient();

  return api.useMutation("put", "/users/{userId}", {
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: ["get", "/users"] }),
        // The caller may have changed their own role or disabled themselves.
        queryClient.invalidateQueries({ queryKey: ["get", "/me"] }),
      ]),
  });
}

export function useResetUserPassword() {
  return api.useMutation("put", "/users/{userId}/password");
}
