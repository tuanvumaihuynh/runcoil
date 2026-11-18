import { createFileRoute, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/_app/projects/$projectId/runs")({
  staticData: { crumb: "Runs" },
  component: Outlet,
});
