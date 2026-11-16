import { FolderKanban, UserCog, type LucideIcon } from "lucide-react";

import type { Permission } from "@/lib/permissions";
import { hasPermission } from "@/lib/permissions";

type AdminSection = {
  title: string;
  description: string;
  icon: LucideIcon;
  to: "/admin/users" | "/admin/projects";
  permission: Permission;
};

// The one place the server admin sections are described. The overview cards, the page
// headers and the sub-nav all read it.
export const adminSections = {
  users: {
    title: "Users",
    description: "Manage who can sign in to this server.",
    icon: UserCog,
    to: "/admin/users",
    permission: "users:read",
  },
  projects: {
    title: "Projects",
    description: "Manage every project on this server.",
    icon: FolderKanban,
    to: "/admin/projects",
    permission: "projects:read",
  },
} as const satisfies Record<string, AdminSection>;

export function visibleAdminSections(permissions: readonly Permission[] | undefined) {
  return Object.values(adminSections).filter((section) =>
    hasPermission(permissions, section.permission),
  );
}
