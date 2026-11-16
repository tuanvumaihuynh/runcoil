import { createFileRoute } from "@tanstack/react-router";

// A pass-through layout: it only adds the "Settings" crumb.
export const Route = createFileRoute("/_app/projects/$projectId/settings")({
  staticData: { crumb: "Settings" },
});
