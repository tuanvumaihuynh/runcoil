import { Ban } from "lucide-react";

import type { components } from "@/api/schema";
import { createDataTableColumnHelper } from "@/components/data-table";
import { RelativeTime } from "@/components/relative-time";
import { RowAction, RowActions } from "@/components/row-action";
import { ApiKeyStatus } from "@/features/api-keys/components/api-key-status";
import { maskedKey } from "@/features/api-keys/schemas";
import { formatDate } from "@/lib/format";
import { cn } from "@/lib/utils";

type ApiKey = components["schemas"]["ApiKey"];

const columnHelper = createDataTableColumnHelper<ApiKey>();

type ApiKeysColumnsOptions = {
  canWrite: boolean;
  now: Date;
  /** Gets the clicked button, so the dialog can give focus back to it. */
  onRevoke: (apiKey: ApiKey, trigger: HTMLElement) => void;
};

export function apiKeysColumns({ canWrite, now, onRevoke }: ApiKeysColumnsOptions) {
  const columns = [
    columnHelper.accessor("name", {
      header: "Name",
      cell: ({ row }) => (
        <span
          className={cn(
            row.original.revokedAt === undefined ? "font-medium" : "text-muted-foreground",
          )}
        >
          {row.original.name}
        </span>
      ),
    }),
    columnHelper.display({
      id: "key",
      header: "Key",
      cell: ({ row }) => <span className="font-mono">{maskedKey(row.original.keyHint)}</span>,
    }),
    columnHelper.display({
      id: "createdBy",
      header: "Created by",
      cell: ({ row }) => row.original.createdBy,
    }),
    columnHelper.accessor("createdAt", {
      header: "Created",
      cell: ({ row }) => formatDate(row.original.createdAt),
    }),
    columnHelper.display({
      id: "lastUsedAt",
      header: "Last used",
      cell: ({ row }) =>
        row.original.lastUsedAt === undefined ? (
          <span className="text-muted-foreground">Never</span>
        ) : (
          <RelativeTime value={row.original.lastUsedAt} now={now} />
        ),
    }),
    columnHelper.display({
      id: "status",
      header: "Status",
      cell: ({ row }) => <ApiKeyStatus revokedAt={row.original.revokedAt} />,
    }),
  ];

  if (canWrite) {
    columns.push(
      columnHelper.display({
        id: "actions",
        header: () => <span className="sr-only">Actions</span>,
        cell: ({ row }) =>
          row.original.revokedAt === undefined && (
            <RowActions>
              <RowAction
                label={`Revoke ${row.original.name}`}
                onClick={(event) => onRevoke(row.original, event.currentTarget)}
              >
                <Ban />
              </RowAction>
            </RowActions>
          ),
      }),
    );
  }

  return columns;
}
