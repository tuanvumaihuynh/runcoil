import { createFileRoute, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/_app/projects/$projectId/tasks")({
  staticData: { crumb: "Tasks" },
  component: Outlet,
});
