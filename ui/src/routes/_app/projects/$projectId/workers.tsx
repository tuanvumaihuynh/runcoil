import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { WorkersPage } from "@/features/workers/components/workers-page";

const workersSearchSchema = z.object({
  state: z.enum(["live", "ended"]).default("live"),
});

export const Route = createFileRoute("/_app/projects/$projectId/workers")({
  staticData: { crumb: "Workers" },
  validateSearch: workersSearchSchema,
  component: WorkersRoute,
});

function WorkersRoute() {
  const { projectId } = Route.useParams();
  const { state } = Route.useSearch();
  const navigate = Route.useNavigate();

  return (
    <WorkersPage
      projectId={projectId}
      state={state}
      onStateChange={(next) => void navigate({ search: { state: next }, replace: true })}
    />
  );
}
