import { createFileRoute, notFound } from "@tanstack/react-router";

import { PagePlaceholder } from "@/components/page-placeholder";
import { settingsSections } from "@/features/settings/sections";
import { hasPermission } from "@/lib/permissions";

export const Route = createFileRoute("/_app/projects/$projectId/settings/api-keys")({
  beforeLoad: ({ context }) => {
    if (!hasPermission(context.project.permissions, "api_keys:read")) {
      throw notFound();
    }
  },
  staticData: { crumb: "API keys" },
  component: ApiKeysPage,
});

function ApiKeysPage() {
  const { title, description } = settingsSections.apiKeys;

  return <PagePlaceholder title={title} description={description} />;
}
