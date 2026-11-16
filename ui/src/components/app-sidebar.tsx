import { useSuspenseQuery } from "@tanstack/react-query";
import { Link, useMatch, useMatchRoute } from "@tanstack/react-router";
import { ListChecks, Play, Server, Settings, Shield } from "lucide-react";
import { useEffect } from "react";

import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  SidebarSeparator,
  SidebarTrigger,
  useSidebar,
} from "@/components/ui/sidebar";
import { ProjectSwitcher } from "@/components/project-switcher";
import { ServerVersion } from "@/components/server-version";
import { meQueryOptions } from "@/features/auth/queries";
import { pickLandingProject, readLastProject, storeLastProject } from "@/lib/last-project";
import { hasPermission } from "@/lib/permissions";

const projectNav = [
  { title: "Runs", to: "/projects/$projectId/runs", icon: Play },
  { title: "Tasks", to: "/projects/$projectId/tasks", icon: ListChecks },
  { title: "Workers", to: "/projects/$projectId/workers", icon: Server },
] as const;

export function AppSidebar() {
  const { data: me } = useSuspenseQuery(meQueryOptions);
  const routeProjectId = useMatch({
    from: "/_app/projects/$projectId",
    shouldThrow: false,
    select: (match) => match.params.projectId,
  });
  const matchRoute = useMatchRoute();
  const { setOpenMobile } = useSidebar();

  // On a project route that project; elsewhere the last one opened, then the first one.
  const routeProject = me.projects.find((project) => project.id === routeProjectId);
  const openedProjectId = routeProject?.id;
  const current = routeProject ?? pickLandingProject(me.projects, readLastProject());

  useEffect(() => {
    if (openedProjectId !== undefined) {
      storeLastProject(openedProjectId);
    }
  }, [openedProjectId]);

  const showAdmin =
    hasPermission(me.permissions, "users:read") || hasPermission(me.permissions, "projects:read");
  const closeMobileNav = () => setOpenMobile(false);

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <div className="flex items-center">
          <ProjectSwitcher current={current} />
          <SidebarTrigger className="shrink-0" />
        </div>
      </SidebarHeader>
      <SidebarContent>
        {current && (
          <SidebarGroup>
            <SidebarGroupContent>
              <SidebarMenu>
                {projectNav.map((item) => (
                  <SidebarMenuItem key={item.to}>
                    <SidebarMenuButton
                      asChild
                      isActive={Boolean(
                        matchRoute({ to: item.to, params: { projectId: current.id }, fuzzy: true }),
                      )}
                      tooltip={item.title}
                    >
                      <Link
                        to={item.to}
                        params={{ projectId: current.id }}
                        onClick={closeMobileNav}
                      >
                        <item.icon />
                        <span>{item.title}</span>
                      </Link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        )}
      </SidebarContent>
      <SidebarSeparator className="data-horizontal:w-auto" />
      <SidebarFooter>
        <SidebarMenu>
          {current && (
            <SidebarMenuItem>
              <SidebarMenuButton
                asChild
                isActive={Boolean(
                  matchRoute({
                    to: "/projects/$projectId/settings",
                    params: { projectId: current.id },
                    fuzzy: true,
                  }),
                )}
                tooltip="Project settings"
              >
                <Link
                  to="/projects/$projectId/settings"
                  params={{ projectId: current.id }}
                  onClick={closeMobileNav}
                >
                  <Settings />
                  <span>Project settings</span>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>
          )}
          {showAdmin && (
            <SidebarMenuItem>
              <SidebarMenuButton
                asChild
                isActive={Boolean(matchRoute({ to: "/admin", fuzzy: true }))}
                tooltip="Server admin"
              >
                <Link to="/admin" onClick={closeMobileNav}>
                  <Shield />
                  <span>Server admin</span>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>
          )}
        </SidebarMenu>
        <ServerVersion />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
