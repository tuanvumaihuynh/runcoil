import { MoreHorizontal } from "lucide-react";

import type { components } from "@/api/schema";
import { createDataTableColumnHelper } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { isRole, roleLabels, roles, type Role } from "@/features/members/schemas";

type Member = components["schemas"]["Member"];

const columnHelper = createDataTableColumnHelper<Member>();

type MembersColumnsOptions = {
  canWrite: boolean;
  onRoleChange: (member: Member, role: Role) => void;
  onRemove: (member: Member) => void;
};

export function membersColumns({ canWrite, onRoleChange, onRemove }: MembersColumnsOptions) {
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
          <Select
            value={member.role}
            onValueChange={(value) => {
              if (isRole(value) && value !== member.role) {
                onRoleChange(member, value);
              }
            }}
          >
            <SelectTrigger size="sm" aria-label={`Role of ${member.username}`}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {roles.map((role) => (
                <SelectItem key={role} value={role}>
                  {roleLabels[role]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
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
          <div className="flex justify-end">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={`Actions for ${row.original.username}`}
                >
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="min-w-40">
                <DropdownMenuItem variant="destructive" onSelect={() => onRemove(row.original)}>
                  Remove
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        ),
      }),
    );
  }

  return columns;
}
