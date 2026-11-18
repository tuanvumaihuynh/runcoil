import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { RunsPage } from "@/features/runs/components/runs-page";
import { runStatuses } from "@/features/runs/status";

const runsSearchSchema = z.object({
  status: z.array(z.enum(runStatuses)).optional(),
});

export const Route = createFileRoute("/_app/projects/$projectId/runs/")({
  validateSearch: runsSearchSchema,
  component: RunsRoute,
});

function RunsRoute() {
  const { projectId } = Route.useParams();
  const { status } = Route.useSearch();
  const navigate = Route.useNavigate();

  return (
    <RunsPage
      projectId={projectId}
      status={status}
      onStatusChange={(next) => void navigate({ search: { status: next }, replace: true })}
    />
  );
}
