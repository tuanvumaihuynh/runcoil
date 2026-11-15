import { useQueryClient } from "@tanstack/react-query";

import { api } from "@/api/client";

export function useMembers(projectId: string) {
  return api.useQuery("get", "/projects/{projectId}/members", {
    params: { path: { projectId } },
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
