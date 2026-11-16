import { createFileRoute } from "@tanstack/react-router";

import { PagePlaceholder } from "@/components/page-placeholder";

export const Route = createFileRoute("/_app/projects/$projectId/runs/")({
  staticData: { crumb: "Runs" },
  component: RunsPage,
});

function RunsPage() {
  return <PagePlaceholder title="Runs" description="Track task runs and inspect their results." />;
}
