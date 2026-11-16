import { useQueryClient } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";

import { api, client } from "@/api/client";
import { meQueryOptions, readCachedMe } from "@/features/auth/queries";
import { hasPermission } from "@/lib/permissions";
import { usePagedQuery } from "@/lib/paged-query";

export function useUsers() {
  return usePagedQuery({
    path: "/users",
    fetchPage: ({ pageSize, pageToken, signal }) =>
      client.GET("/users", { params: { query: { pageSize, pageToken } }, signal }),
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
  const router = useRouter();

  return api.useMutation("put", "/users/{userId}", {
    onSuccess: async (_data, { params }) => {
      // The caller may have changed their own role or disabled themselves.
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["get", "/users"] }),
        queryClient.invalidateQueries({ queryKey: meQueryOptions.queryKey }),
      ]);

      const me = readCachedMe(queryClient);
      if (me === undefined || me.user.id !== params.path.userId) {
        return;
      }

      if (hasPermission(me.permissions, "users:read")) {
        // The route context holds permissions from beforeLoad. Rerun it.
        void router.invalidate();
      } else {
        // They can no longer read users, so this page is not theirs anymore.
        void router.navigate({ to: "/" });
      }
    },
  });
}

export function useResetUserPassword() {
  return api.useMutation("put", "/users/{userId}/password");
}
