import { createFileRoute } from "@tanstack/react-router";

import { TaskPage } from "@/features/tasks/components/task-page";

export const Route = createFileRoute("/_app/projects/$projectId/tasks/$taskId")({
  staticData: { crumb: "Task" },
  component: TaskRoute,
});

function TaskRoute() {
  const { projectId, taskId } = Route.useParams();

  return <TaskPage projectId={projectId} taskId={taskId} />;
}
