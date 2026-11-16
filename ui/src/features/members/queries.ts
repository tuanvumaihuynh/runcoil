import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";

import { api, client } from "@/api/client";
import { meQueryOptions, readCachedMe } from "@/features/auth/queries";
import type { components } from "@/api/schema";
import { usePagedQuery } from "@/lib/paged-query";

type MemberList = components["schemas"]["MemberList"];

export function useMembers(projectId: string) {
  return api.useQuery("get", "/projects/{projectId}/members", {
    params: { path: { projectId } },
  });
}

export function useUserLookup(enabled: boolean) {
  return usePagedQuery({
    path: "/users/lookup",
    fetchPage: ({ pageSize, pageToken, signal }) =>
      client.GET("/users/lookup", { params: { query: { pageSize, pageToken } }, signal }),
    enabled,
  });
}

function invalidateMembers(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: ["get", "/projects/{projectId}/members"] });
}

function invalidateMe(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: meQueryOptions.queryKey });
}

function isCurrentUser(queryClient: QueryClient, userId: string) {
  return readCachedMe(queryClient)?.user.id === userId;
}

function membersQueryKey(projectId: string) {
  return api.queryOptions("get", "/projects/{projectId}/members", {
    params: { path: { projectId } },
  }).queryKey;
}

export function useAddMember() {
  const queryClient = useQueryClient();

  return api.useMutation("post", "/projects/{projectId}/members", {
    onSuccess: () => invalidateMembers(queryClient),
  });
}

export function useUpdateMember() {
  const queryClient = useQueryClient();
  const router = useRouter();

  return api.useMutation("put", "/projects/{projectId}/members/{userId}", {
    // Show the picked role at once. A role select that waits for the server looks like it ignored the pick.
    onMutate: async ({ params, body }) => {
      const key = membersQueryKey(params.path.projectId);
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<MemberList>(key);
      queryClient.setQueryData<MemberList>(key, (old) =>
        old === undefined
          ? old
          : {
              ...old,
              members: old.members.map((member) =>
                member.userId === params.path.userId ? { ...member, role: body.role } : member,
              ),
            },
      );
      return { key, previous };
    },
    onError: (_error, _variables, context) => {
      if (context?.previous !== undefined) {
        queryClient.setQueryData(context.key, context.previous);
      }
      // Another member's update may have started from the list this one rolled back. Reload it.
      void invalidateMembers(queryClient);
    },
    onSuccess: async (_data, { params }) => {
      await Promise.all([invalidateMembers(queryClient), invalidateMe(queryClient)]);
      if (isCurrentUser(queryClient, params.path.userId)) {
        // The route context holds the project's permissions from beforeLoad. Rerun it.
        void router.invalidate();
      }
    },
  });
}

export function useRemoveMember() {
  const queryClient = useQueryClient();

  return api.useMutation("delete", "/projects/{projectId}/members/{userId}", {
    onSuccess: async (_data, { params }) => {
      if (isCurrentUser(queryClient, params.path.userId)) {
        // The caller left the project. Its member list is no longer theirs to read. The dialog
        // navigates away once this settles.
        await invalidateMe(queryClient);
        return;
      }
      await Promise.all([invalidateMembers(queryClient), invalidateMe(queryClient)]);
    },
  });
}
