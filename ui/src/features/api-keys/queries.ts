import { useQueryClient, type QueryClient } from "@tanstack/react-query";

import { api } from "@/api/client";

export function useApiKeys(projectId: string) {
  return api.useQuery("get", "/projects/{projectId}/api-keys", {
    params: { path: { projectId } },
  });
}

function invalidateApiKeys(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: ["get", "/projects/{projectId}/api-keys"] });
}

export function useCreateApiKey() {
  const queryClient = useQueryClient();

  return api.useMutation("post", "/projects/{projectId}/api-keys", {
    onSuccess: () => invalidateApiKeys(queryClient),
  });
}

export function useRevokeApiKey() {
  const queryClient = useQueryClient();

  return api.useMutation("post", "/projects/{projectId}/api-keys/{apiKeyId}/revoke", {
    onSuccess: () => invalidateApiKeys(queryClient),
  });
}
