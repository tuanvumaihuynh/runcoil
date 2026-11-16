import { useSuspenseQuery } from "@tanstack/react-query";
import { Link, useMatch, useMatchRoute } from "@tanstack/react-router";
import {
  CalendarClock,
  FolderKanban,
  ListChecks,
  Play,
  Server,
  UserCog,
  Users,
} from "lucide-react";

import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  useSidebar,
} from "@/components/ui/sidebar";
import { ProjectSwitcher } from "@/components/project-switcher";
import { Wordmark } from "@/components/wordmark";
import { meQueryOptions } from "@/features/auth/queries";
import { hasPermission } from "@/lib/permissions";

const projectNav = [
  { title: "Runs", to: "/projects/$projectId/runs", icon: Play },
  { title: "Tasks", to: "/projects/$projectId/tasks", icon: ListChecks },
  { title: "Schedules", to: "/projects/$projectId/schedules", icon: CalendarClock },
  { title: "Workers", to: "/projects/$projectId/workers", icon: Server },
] as const;

const projectSettingsNav = [
  { title: "Members", to: "/projects/$projectId/settings/members", icon: Users },
] as const;

const adminNav = [
  { title: "Users", to: "/admin/users", icon: UserCog, permission: "users:read" },
  { title: "Projects", to: "/admin/projects", icon: FolderKanban, permission: "projects:read" },
] as const;

type ProjectNavItem = (typeof projectNav)[number] | (typeof projectSettingsNav)[number];

function ProjectNavMenu({
  items,
  projectId,
  onNavigate,
}: {
  items: readonly ProjectNavItem[];
  projectId: string;
  onNavigate: () => void;
}) {
  const matchRoute = useMatchRoute();

  return (
    <SidebarMenu>
      {items.map((item) => (
        <SidebarMenuItem key={item.to}>
          <SidebarMenuButton
            asChild
            isActive={Boolean(matchRoute({ to: item.to, params: { projectId }, fuzzy: true }))}
            tooltip={item.title}
          >
            <Link to={item.to} params={{ projectId }} onClick={onNavigate}>
              <item.icon />
              <span>{item.title}</span>
            </Link>
          </SidebarMenuButton>
        </SidebarMenuItem>
      ))}
    </SidebarMenu>
  );
}

export function AppSidebar() {
  const { data: me } = useSuspenseQuery(meQueryOptions);
  const projectId = useMatch({
    from: "/_app/projects/$projectId",
    shouldThrow: false,
    select: (match) => match.params.projectId,
  });
  const matchRoute = useMatchRoute();
  const { setOpenMobile } = useSidebar();
  const current =
    projectId === undefined
      ? me.projects[0]
      : me.projects.find((project) => project.id === projectId);
  const settingsItems: readonly ProjectNavItem[] =
    current && hasPermission(current.permissions, "members:read") ? projectSettingsNav : [];
  const adminItems = adminNav.filter((item) => hasPermission(me.permissions, item.permission));
  const closeMobileNav = () => setOpenMobile(false);

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" asChild>
              <Link to="/" onClick={() => setOpenMobile(false)}>
                <div className="flex aspect-square size-8 items-center justify-center">
                  <img src="/runcoil-mark-light.svg" alt="" className="size-6 dark:hidden" />
                  <img src="/runcoil-mark.svg" alt="" className="hidden size-6 dark:block" />
                </div>
                <Wordmark className="h-8! w-auto!" />
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
        <ProjectSwitcher projects={me.projects} current={current} />
      </SidebarHeader>
      <SidebarContent>
        {current && (
          <SidebarGroup>
            <SidebarGroupContent>
              <ProjectNavMenu
                items={projectNav}
                projectId={current.id}
                onNavigate={closeMobileNav}
              />
            </SidebarGroupContent>
          </SidebarGroup>
        )}
        {current && settingsItems.length > 0 && (
          <SidebarGroup>
            <SidebarGroupLabel>Settings</SidebarGroupLabel>
            <SidebarGroupContent>
              <ProjectNavMenu
                items={settingsItems}
                projectId={current.id}
                onNavigate={closeMobileNav}
              />
            </SidebarGroupContent>
          </SidebarGroup>
        )}
        {adminItems.length > 0 && (
          <SidebarGroup>
            <SidebarGroupLabel>Admin</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {adminItems.map((item) => (
                  <SidebarMenuItem key={item.to}>
                    <SidebarMenuButton
                      asChild
                      isActive={Boolean(matchRoute({ to: item.to, fuzzy: true }))}
                      tooltip={item.title}
                    >
                      <Link to={item.to} onClick={() => setOpenMobile(false)}>
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
      <SidebarRail />
    </Sidebar>
  );
}
