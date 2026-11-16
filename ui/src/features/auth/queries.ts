import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { toast } from "sonner";

import type { components } from "@/api/schema";
import { api } from "@/api/client";
import { isUnauthenticatedError } from "@/lib/api-errors";

export const meQueryOptions = api.queryOptions("get", "/me");

// The cached /me, without a request. The query options do not carry the response type.
export function readCachedMe(queryClient: QueryClient) {
  return queryClient.getQueryData<components["schemas"]["Me"]>(meQueryOptions.queryKey);
}

export function useLogin() {
  return api.useMutation("post", "/auth/login");
}

export function useLogout() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  function signedOut() {
    queryClient.clear();
    void navigate({ to: "/login" });
  }

  return api.useMutation("post", "/auth/logout", {
    onSuccess: signedOut,
    onError: (error) => {
      if (isUnauthenticatedError(error)) {
        signedOut();
        return;
      }

      toast.error("Could not sign out");
    },
  });
}

export function useChangePassword() {
  return api.useMutation("put", "/me/password");
}
