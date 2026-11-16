import { useSuspenseQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ChevronsUpDown, Folders, Plus } from "lucide-react";
import { useState } from "react";

import type { components } from "@/api/schema";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";
import { meQueryOptions } from "@/features/auth/queries";
import { CreateProjectDialog } from "@/features/projects/components/create-project-dialog";
import { roleLabels } from "@/features/members/schemas";
import { hasPermission } from "@/lib/permissions";

type ProjectAccess = components["schemas"]["ProjectAccess"];

type ProjectSwitcherProps = {
  current: ProjectAccess | undefined;
};

export function ProjectSwitcher({ current }: ProjectSwitcherProps) {
  const { data: me } = useSuspenseQuery(meQueryOptions);
  const navigate = useNavigate();
  const { setOpenMobile } = useSidebar();
  const [open, setOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);

  const canListProjects = hasPermission(me.permissions, "projects:read");
  const canCreateProject = hasPermission(me.permissions, "projects:create");

  function openProject(projectId: string) {
    setOpen(false);
    setOpenMobile(false);
    void navigate({ to: "/projects/$projectId/runs", params: { projectId } });
  }

  return (
    <>
      <SidebarMenu className="min-w-0 flex-1 overflow-hidden">
        <SidebarMenuItem>
          <Popover open={open} onOpenChange={setOpen}>
            <PopoverTrigger asChild>
              <SidebarMenuButton
                size="lg"
                aria-label="Switch project"
                className="group-data-[collapsible=icon]:size-full! group-data-[collapsible=icon]:h-12! group-data-[collapsible=icon]:p-2!"
              >
                <div className="flex size-6 shrink-0 items-center justify-center rounded-md bg-muted text-xs font-medium text-muted-foreground">
                  {current?.name.charAt(0).toUpperCase() ?? "?"}
                </div>
                <span className="grid min-w-0 flex-1 text-left leading-tight whitespace-nowrap">
                  <span className="truncate font-medium">{current?.name ?? "No project"}</span>
                  {current && (
                    <span className="truncate text-xs text-muted-foreground">
                      {roleLabels[current.role]}
                    </span>
                  )}
                </span>
                <ChevronsUpDown className="ml-auto" />
              </SidebarMenuButton>
            </PopoverTrigger>
            <PopoverContent align="start" className="w-64 gap-0 p-0">
              <Command>
                <CommandInput placeholder="Find a project" />
                <CommandList>
                  <CommandEmpty>No project found.</CommandEmpty>
                  <CommandGroup>
                    {me.projects.map((project) => (
                      <CommandItem
                        key={project.id}
                        value={project.name}
                        data-checked={project.id === current?.id}
                        onSelect={() => openProject(project.id)}
                      >
                        <span className="min-w-0 flex-1 truncate">{project.name}</span>
                        <span className="text-xs text-muted-foreground">
                          {roleLabels[project.role]}
                        </span>
                      </CommandItem>
                    ))}
                  </CommandGroup>
                  {(canListProjects || canCreateProject) && <CommandSeparator />}
                  {(canListProjects || canCreateProject) && (
                    <CommandGroup forceMount>
                      {canListProjects && (
                        <CommandItem
                          forceMount
                          value="All projects"
                          onSelect={() => {
                            setOpen(false);
                            setOpenMobile(false);
                            void navigate({ to: "/admin/projects" });
                          }}
                        >
                          <Folders />
                          All projects
                        </CommandItem>
                      )}
                      {canCreateProject && (
                        <CommandItem
                          forceMount
                          value="New project"
                          onSelect={() => {
                            setOpen(false);
                            setCreateOpen(true);
                          }}
                        >
                          <Plus />
                          New project
                        </CommandItem>
                      )}
                    </CommandGroup>
                  )}
                </CommandList>
              </Command>
            </PopoverContent>
          </Popover>
        </SidebarMenuItem>
      </SidebarMenu>
      {canCreateProject && (
        <CreateProjectDialog
          open={createOpen}
          onOpenChange={setCreateOpen}
          onCreated={(project) => openProject(project.id)}
        />
      )}
    </>
  );
}
