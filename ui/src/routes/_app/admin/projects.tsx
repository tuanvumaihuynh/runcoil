import { createFileRoute, notFound } from "@tanstack/react-router";

import { meQueryOptions } from "@/features/auth/queries";
import { ProjectsPage } from "@/features/projects/components/projects-page";
import { hasPermission } from "@/lib/permissions";

export const Route = createFileRoute("/_app/admin/projects")({
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.ensureQueryData(meQueryOptions);

    if (!hasPermission(me.permissions, "projects:read")) {
      throw notFound();
    }
  },
  component: ProjectsPage,
});
