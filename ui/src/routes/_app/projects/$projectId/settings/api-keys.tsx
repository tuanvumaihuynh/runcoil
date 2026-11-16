import { createFileRoute, notFound } from "@tanstack/react-router";

import { ApiKeysPage } from "@/features/api-keys/components/api-keys-page";
import { hasPermission } from "@/lib/permissions";

export const Route = createFileRoute("/_app/projects/$projectId/settings/api-keys")({
  beforeLoad: ({ context }) => {
    if (!hasPermission(context.project.permissions, "api_keys:read")) {
      throw notFound();
    }
  },
  staticData: { crumb: "API keys" },
  component: ApiKeysRoute,
});

function ApiKeysRoute() {
  const { project } = Route.useRouteContext();

  return <ApiKeysPage project={project} />;
}
