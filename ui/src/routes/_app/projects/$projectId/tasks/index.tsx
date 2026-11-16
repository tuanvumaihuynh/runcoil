import { createFileRoute } from "@tanstack/react-router";

import { PagePlaceholder } from "@/components/page-placeholder";

export const Route = createFileRoute("/_app/projects/$projectId/tasks/")({
  staticData: { crumb: "Tasks" },
  component: TasksPage,
});

function TasksPage() {
  return (
    <PagePlaceholder title="Tasks" description="Browse the tasks your workers have registered." />
  );
}
