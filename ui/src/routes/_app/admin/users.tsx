import { createFileRoute, notFound } from "@tanstack/react-router";

import { meQueryOptions } from "@/features/auth/queries";
import { UsersPage } from "@/features/users/components/users-page";
import { hasPermission } from "@/lib/permissions";

export const Route = createFileRoute("/_app/admin/users")({
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.ensureQueryData(meQueryOptions);

    if (!hasPermission(me.permissions, "users:read")) {
      throw notFound();
    }
  },
  staticData: { crumb: "Users" },
  component: UsersPage,
});
