import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";

import { PageHeader } from "@/components/page-header";
import { SectionCardContent, SectionCardGrid } from "@/components/section-card";
import { Item } from "@/components/ui/item";
import { visibleAdminSections } from "@/features/admin/sections";
import { meQueryOptions } from "@/features/auth/queries";

export const Route = createFileRoute("/_app/admin/")({
  component: AdminOverview,
});

function AdminOverview() {
  const { data: me } = useSuspenseQuery(meQueryOptions);
  const sections = visibleAdminSections(me.permissions);

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader
        title="Server admin"
        description="Manage users and projects across this server."
      />

      <SectionCardGrid>
        {sections.map((section) => (
          <Item key={section.to} variant="outline" asChild>
            <Link to={section.to}>
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
