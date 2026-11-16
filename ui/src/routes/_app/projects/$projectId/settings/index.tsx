import { createFileRoute, Link } from "@tanstack/react-router";

import { PageHeader } from "@/components/page-header";
import { SectionCardContent, SectionCardGrid } from "@/components/section-card";
import { Item } from "@/components/ui/item";
import { visibleSettingsSections } from "@/features/settings/sections";

export const Route = createFileRoute("/_app/projects/$projectId/settings/")({
  component: SettingsOverview,
});

function SettingsOverview() {
  const { project } = Route.useRouteContext();
  const sections = visibleSettingsSections(project.permissions);

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader
        title="Project settings"
        description="Manage how your project is set up and who can access it."
      />

      <SectionCardGrid>
        {sections.map((section) => (
          <Item key={section.to} variant="outline" asChild>
            <Link to={section.to} params={{ projectId: project.id }}>
              <SectionCardContent
                icon={section.icon}
                title={section.title}
                description={section.description}
              />
            </Link>
          </Item>
        ))}
      </SectionCardGrid>
    </div>
  );
}
