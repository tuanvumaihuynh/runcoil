import { useQueryClient } from "@tanstack/react-query";

import { api } from "@/api/client";

export function useProjects() {
  return api.useQuery("get", "/projects");
}

export function useCreateProject() {
  const queryClient = useQueryClient();

  return api.useMutation("post", "/projects", {
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: ["get", "/projects"] }),
        // The project switcher reads the projects the caller can open.
        queryClient.invalidateQueries({ queryKey: ["get", "/me"] }),
      ]),
  });
}
