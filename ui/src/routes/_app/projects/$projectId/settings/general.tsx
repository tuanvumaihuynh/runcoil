import { createFileRoute } from "@tanstack/react-router";
import { Copy } from "lucide-react";
import { toast } from "sonner";

import { PageHeader } from "@/components/page-header";
import { SettingsRow } from "@/components/settings-row";
import { Button } from "@/components/ui/button";
import { settingsSections } from "@/features/settings/sections";

export const Route = createFileRoute("/_app/projects/$projectId/settings/general")({
  staticData: { crumb: "General" },
  component: GeneralPage,
});

function GeneralPage() {
  const { project } = Route.useRouteContext();
  const { title, description } = settingsSections.general;

  async function copyId() {
    try {
      await navigator.clipboard.writeText(project.id);
      toast.success("Copied");
    } catch {
      toast.error("Could not copy");
    }
  }

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader title={title} description={description} />

      <div className="flex flex-col">
        <SettingsRow label="Name">{project.name}</SettingsRow>
        <SettingsRow label="Project ID">
          <div className="flex items-center gap-2">
            <span className="font-mono break-all">{project.id}</span>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Copy project ID"
              onClick={() => void copyId()}
            >
              <Copy />
            </Button>
          </div>
        </SettingsRow>
      </div>
    </div>
  );
}
