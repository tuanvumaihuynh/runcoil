import { useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { MoreHorizontal } from "lucide-react";
import { useMemo } from "react";

import type { components } from "@/api/schema";
import { createDataTableColumnHelper, DataTable } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { CreateProjectDialog } from "@/features/projects/components/create-project-dialog";
import { meQueryOptions } from "@/features/auth/queries";
import { useProjects } from "@/features/projects/queries";
import { formatDate } from "@/lib/format";
import { hasPermission } from "@/lib/permissions";

type Project = components["schemas"]["Project"];

const columnHelper = createDataTableColumnHelper<Project>();

const columns = [
  columnHelper.accessor("name", {
    header: "Name",
    cell: ({ row }) => (
      <Link
        to="/projects/$projectId/runs"
        params={{ projectId: row.original.id }}
        className="font-medium hover:underline"
      >
        {row.original.name}
      </Link>
    ),
  }),
  columnHelper.display({
    id: "created",
    header: "Created",
    cell: ({ row }) => formatDate(row.original.createdAt),
  }),
  columnHelper.display({
    id: "actions",
    header: () => <span className="sr-only">Actions</span>,
    cell: ({ row }) => (
      <div className="flex justify-end">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" aria-label={`Actions for ${row.original.name}`}>
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="min-w-40">
            <DropdownMenuItem asChild>
              <Link
                to="/projects/$projectId/settings/members"
                params={{ projectId: row.original.id }}
              >
                Members
              </Link>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    ),
  }),
];

export function ProjectsPage() {
  const { data: me } = useSuspenseQuery(meQueryOptions);
  const canCreate = hasPermission(me.permissions, "projects:create");
  const projects = useProjects();
  const rows = useMemo(() => projects.data?.projects ?? [], [projects.data]);

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader
        title="Projects"
        description="Every project on this server."
        actions={canCreate && <CreateProjectDialog />}
      />

      {projects.isError ? (
        <div className="flex flex-col items-start gap-3">
          <p role="alert" className="text-sm text-destructive">
            Could not load projects.
          </p>
          <Button variant="outline" onClick={() => void projects.refetch()}>
            Try again
          </Button>
        </div>
      ) : (
        <DataTable
          columns={columns}
          data={rows}
          loading={projects.isPending}
          emptyMessage="No projects yet."
        />
      )}
    </div>
  );
}
