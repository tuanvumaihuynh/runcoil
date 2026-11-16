import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, notFound, Outlet, useMatch } from "@tanstack/react-router";

import { SectionLayout } from "@/components/section-layout";
import { visibleAdminSections } from "@/features/admin/sections";
import { meQueryOptions } from "@/features/auth/queries";
import { hasPermission } from "@/lib/permissions";

export const Route = createFileRoute("/_app/admin")({
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.ensureQueryData(meQueryOptions);

    if (
      !hasPermission(me.permissions, "users:read") &&
      !hasPermission(me.permissions, "projects:read")
    ) {
      throw notFound();
    }
  },
  staticData: { crumb: "Server admin" },
  component: AdminLayout,
});

function AdminLayout() {
  const { data: me } = useSuspenseQuery(meQueryOptions);
  const isOverview = useMatch({ from: "/_app/admin/", shouldThrow: false });

  // The overview lists the sections as cards, so it takes the full width without the sub-nav.
  if (isOverview) {
    return <Outlet />;
  }

  return (
    <SectionLayout label="Server admin" sections={visibleAdminSections(me.permissions)}>
      <Outlet />
    </SectionLayout>
  );
}
