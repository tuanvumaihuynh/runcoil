import { useMemo, useRef, useState } from "react";

import type { components } from "@/api/schema";
import { DataTable } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { apiKeysColumns } from "@/features/api-keys/components/api-keys-columns";
import { CreateApiKeyDialog } from "@/features/api-keys/components/create-api-key-dialog";
import { RevokeApiKeyDialog } from "@/features/api-keys/components/revoke-api-key-dialog";
import { useApiKeys } from "@/features/api-keys/queries";
import { settingsSections } from "@/features/settings/sections";
import { hasPermission } from "@/lib/permissions";

type ProjectAccess = components["schemas"]["ProjectAccess"];
type ApiKey = components["schemas"]["ApiKey"];

type ApiKeysPageProps = {
  project: ProjectAccess;
};

export function ApiKeysPage({ project }: ApiKeysPageProps) {
  const canWrite = hasPermission(project.permissions, "api_keys:write");
  const apiKeys = useApiKeys(project.id);
  // The Revoke button comes along, so the dialog can give focus back to it.
  const [revoking, setRevoking] = useState<{ apiKey: ApiKey; trigger: HTMLElement }>();
  const createButtonRef = useRef<HTMLButtonElement>(null);

  const rows = useMemo(() => apiKeys.data?.apiKeys ?? [], [apiKeys.data]);
  const columns = useMemo(
    () =>
      apiKeysColumns({
        canWrite,
        onRevoke: (apiKey, trigger) => setRevoking({ apiKey, trigger }),
      }),
    [canWrite],
  );

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader
        title={settingsSections.apiKeys.title}
        description={settingsSections.apiKeys.description}
        actions={
          canWrite && <CreateApiKeyDialog projectId={project.id} triggerRef={createButtonRef} />
        }
      />

      {apiKeys.isError ? (
        <div className="flex flex-col items-start gap-3">
          <p role="alert" className="text-sm text-destructive">
            Could not load API keys.
          </p>
          <Button variant="outline" onClick={() => void apiKeys.refetch()}>
            Try again
          </Button>
        </div>
      ) : (
        <DataTable
          columns={columns}
          data={rows}
          loading={apiKeys.isPending}
          emptyMessage="No API keys yet."
        />
      )}

      {revoking && (
        <RevokeApiKeyDialog
          key={revoking.apiKey.id}
          projectId={project.id}
          apiKey={revoking.apiKey}
          trigger={revoking.trigger}
          fallbackFocusRef={createButtonRef}
          onClose={() => setRevoking(undefined)}
        />
      )}
    </div>
  );
}
