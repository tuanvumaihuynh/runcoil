import { createFileRoute } from "@tanstack/react-router";

import { RunPage } from "@/features/runs/components/run-page";

export const Route = createFileRoute("/_app/projects/$projectId/runs/$runId")({
  staticData: { crumb: "Run" },
  component: RunRoute,
});

function RunRoute() {
  const { projectId, runId } = Route.useParams();

  return <RunPage projectId={projectId} runId={runId} />;
}
