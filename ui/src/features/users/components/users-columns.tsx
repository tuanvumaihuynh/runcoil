import { KeyRound, Pencil } from "lucide-react";

import type { components } from "@/api/schema";
import { createDataTableColumnHelper } from "@/components/data-table";
import { RowAction, RowActions } from "@/components/row-action";
import { formatDate } from "@/lib/format";

import { UserStatus } from "./user-status";

type ManagedUser = components["schemas"]["ManagedUser"];

const columnHelper = createDataTableColumnHelper<ManagedUser>();

type UsersColumnsOptions = {
  canWrite: boolean;
  onEdit: (user: ManagedUser) => void;
  onResetPassword: (user: ManagedUser) => void;
};

export function usersColumns({ canWrite, onEdit, onResetPassword }: UsersColumnsOptions) {
  const columns = [
    columnHelper.accessor("username", {
      header: "Username",
      cell: ({ row }) => (
        <div className="flex flex-col">
          <span className="font-medium">{row.original.username}</span>
          {row.original.email && (
            <span className="text-xs text-muted-foreground">{row.original.email}</span>
          )}
        </div>
      ),
    }),
    columnHelper.display({
      id: "status",
      header: "Status",
      cell: ({ row }) => <UserStatus disabledAt={row.original.disabledAt} />,
    }),
    columnHelper.display({
      id: "created",
      header: "Created",
      cell: ({ row }) => formatDate(row.original.createdAt),
    }),
  ];

  if (canWrite) {
    columns.push(
      columnHelper.display({
        id: "actions",
        header: () => <span className="sr-only">Actions</span>,
        cell: ({ row }) => (
          <RowActions>
            <RowAction label={`Edit ${row.original.username}`} onClick={() => onEdit(row.original)}>
              <Pencil />
            </RowAction>
            <RowAction
              label={`Reset password for ${row.original.username}`}
              onClick={() => onResetPassword(row.original)}
            >
              <KeyRound />
            </RowAction>
          </RowActions>
        ),
      }),
    );
  }

  return columns;
}
