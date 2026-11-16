import { createFileRoute, redirect } from "@tanstack/react-router";

import { meQueryOptions } from "@/features/auth/queries";
import { pickLandingProject, readLastProject } from "@/lib/last-project";

export const Route = createFileRoute("/_app/")({
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.ensureQueryData(meQueryOptions);
    const project = pickLandingProject(me.projects, readLastProject());

    if (project === undefined) {
      throw redirect({ to: "/account" });
    }

    throw redirect({ to: "/projects/$projectId/runs", params: { projectId: project.id } });
  },
});
