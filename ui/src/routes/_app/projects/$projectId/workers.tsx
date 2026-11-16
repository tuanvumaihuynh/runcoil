import { createFileRoute } from "@tanstack/react-router";

import { PagePlaceholder } from "@/components/page-placeholder";

export const Route = createFileRoute("/_app/projects/$projectId/workers")({
  staticData: { crumb: "Workers" },
  component: WorkersPage,
});

function WorkersPage() {
  return (
    <PagePlaceholder title="Workers" description="Monitor the workers connected to your project." />
  );
}
