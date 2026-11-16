import { KeyRound, SlidersHorizontal, Users, type LucideIcon } from "lucide-react";

import type { Permission } from "@/lib/permissions";
import { hasPermission } from "@/lib/permissions";

type SettingsSection = {
  title: string;
  description: string;
  icon: LucideIcon;
  to:
    | "/projects/$projectId/settings/general"
    | "/projects/$projectId/settings/members"
    | "/projects/$projectId/settings/api-keys";
  // Without a permission the section is for everyone in the project.
  permission?: Permission;
};

// The one place the project settings sections are described. The overview cards, the page
// headers and the sub-nav all read it.
export const settingsSections = {
  general: {
    title: "General",
    description: "View your project's name and the ID your SDK uses.",
    icon: SlidersHorizontal,
    to: "/projects/$projectId/settings/general",
  },
  members: {
    title: "Members",
    description: "Manage who can access your project and their roles.",
    icon: Users,
    to: "/projects/$projectId/settings/members",
    permission: "members:read",
  },
  apiKeys: {
    title: "API keys",
    description: "Manage API keys and their access to your project.",
    icon: KeyRound,
    to: "/projects/$projectId/settings/api-keys",
    permission: "api_keys:read",
  },
} as const satisfies Record<string, SettingsSection>;

export function visibleSettingsSections(permissions: readonly Permission[] | undefined) {
  return Object.values(settingsSections).filter(
    (section) => !("permission" in section) || hasPermission(permissions, section.permission),
  );
}
