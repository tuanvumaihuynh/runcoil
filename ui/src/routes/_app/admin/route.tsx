import { createFileRoute } from "@tanstack/react-router";

// A pass-through layout: it only adds the "Admin" crumb.
export const Route = createFileRoute("/_app/admin")({
  staticData: { crumb: "Admin" },
});
