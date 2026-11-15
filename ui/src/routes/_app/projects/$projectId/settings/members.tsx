import { createFileRoute, notFound } from "@tanstack/react-router";

import { MembersPage } from "@/features/members/components/members-page";
import { hasPermission } from "@/lib/permissions";

export const Route = createFileRoute("/_app/projects/$projectId/settings/members")({
  beforeLoad: ({ context }) => {
    if (!hasPermission(context.project.permissions, "members:read")) {
      throw notFound();
    }
  },
  component: MembersRoute,
});

function MembersRoute() {
  const { project } = Route.useRouteContext();

  return <MembersPage project={project} />;
}
