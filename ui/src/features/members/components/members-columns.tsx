import { Trash2 } from "lucide-react";

import type { components } from "@/api/schema";
import { createDataTableColumnHelper } from "@/components/data-table";
import { RowAction, RowActions } from "@/components/row-action";
import { Badge } from "@/components/ui/badge";
import { RoleSelect } from "@/features/members/components/role-select";
import { roleLabels, type Role } from "@/features/members/schemas";

type Member = components["schemas"]["Member"];

const columnHelper = createDataTableColumnHelper<Member>();

type MembersColumnsOptions = {
  canWrite: boolean;
  /** Members whose role update is still running. Their select stays disabled. */
  updatingUserIds: ReadonlySet<string>;
  onRoleChange: (member: Member, role: Role) => void;
  /** Gets the clicked button, so the dialog can give focus back to it. */
  onRemove: (member: Member, trigger: HTMLElement) => void;
};

export function membersColumns({
  canWrite,
  updatingUserIds,
  onRoleChange,
  onRemove,
}: MembersColumnsOptions) {
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
      id: "role",
      header: "Role",
      cell: ({ row }) => {
        const member = row.original;
        if (!canWrite) {
          return <Badge variant="outline">{roleLabels[member.role]}</Badge>;
        }

        return (
          <RoleSelect
            size="sm"
            aria-label={`Role of ${member.username}`}
            value={member.role}
            disabled={updatingUserIds.has(member.userId)}
            onValueChange={(role) => {
              if (role !== member.role) {
                onRoleChange(member, role);
              }
            }}
          />
        );
      },
    }),
  ];

  if (canWrite) {
    columns.push(
      columnHelper.display({
        id: "actions",
        header: () => <span className="sr-only">Actions</span>,
        cell: ({ row }) => (
          <RowActions>
            <RowAction
              label={`Remove ${row.original.username}`}
              onClick={(event) => onRemove(row.original, event.currentTarget)}
            >
              <Trash2 />
            </RowAction>
          </RowActions>
        ),
      }),
    );
  }

  return columns;
}
