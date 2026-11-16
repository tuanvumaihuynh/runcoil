import { createFileRoute } from "@tanstack/react-router";

import { PagePlaceholder } from "@/components/page-placeholder";

export const Route = createFileRoute("/_app/projects/$projectId/schedules")({
  staticData: { crumb: "Schedules" },
  component: SchedulesPage,
});

function SchedulesPage() {
  return <PagePlaceholder title="Schedules" description="Run tasks on a recurring schedule." />;
}
