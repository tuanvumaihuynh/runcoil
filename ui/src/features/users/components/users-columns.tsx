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
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatDate } from "@/lib/format";

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
      id: "role",
      header: "Role",
      cell: ({ row }) =>
        row.original.isServerAdmin ? <Badge variant="secondary">Server admin</Badge> : null,
    }),
    columnHelper.display({
      id: "status",
      header: "Status",
      cell: ({ row }) => {
        const disabledAt = row.original.disabledAt;
        if (disabledAt === undefined) {
          return <span className="text-muted-foreground">Active</span>;
        }

        return (
          <Tooltip>
            <TooltipTrigger asChild>
              <Badge variant="destructive" tabIndex={0}>
                Disabled
              </Badge>
            </TooltipTrigger>
            <TooltipContent>Disabled on {formatDate(disabledAt)}</TooltipContent>
          </Tooltip>
        );
      },
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
                <DropdownMenuItem onSelect={() => onEdit(row.original)}>Edit</DropdownMenuItem>
                <DropdownMenuItem onSelect={() => onResetPassword(row.original)}>
                  Reset password
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
