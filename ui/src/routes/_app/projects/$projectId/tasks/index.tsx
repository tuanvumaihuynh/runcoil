import { createFileRoute } from "@tanstack/react-router";

import { TasksPage } from "@/features/tasks/components/tasks-page";

export const Route = createFileRoute("/_app/projects/$projectId/tasks/")({
  component: TasksRoute,
});

function TasksRoute() {
  const { projectId } = Route.useParams();

  return <TasksPage projectId={projectId} />;
}
