import { createFileRoute, Outlet, useMatch } from "@tanstack/react-router";

import { SectionLayout } from "@/components/section-layout";
import { visibleSettingsSections } from "@/features/settings/sections";

export const Route = createFileRoute("/_app/projects/$projectId/settings")({
  staticData: { crumb: "Project settings" },
  component: SettingsLayout,
});

function SettingsLayout() {
  const { project } = Route.useRouteContext();
  const isOverview = useMatch({
    from: "/_app/projects/$projectId/settings/",
    shouldThrow: false,
  });

  // The overview lists the sections as cards, so it takes the full width without the sub-nav.
  if (isOverview) {
    return <Outlet />;
  }

  return (
    <SectionLayout
      label="Project settings"
      sections={visibleSettingsSections(project.permissions)}
      params={{ projectId: project.id }}
    >
      <Outlet />
    </SectionLayout>
  );
}
