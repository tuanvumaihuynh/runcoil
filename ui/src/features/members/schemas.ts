import { z } from "zod";

import type { components } from "@/api/schema";

export type Role = components["schemas"]["Role"];

export const roles: readonly Role[] = ["admin", "editor", "viewer"];

export function isRole(value: string): value is Role {
  return (roles as readonly string[]).includes(value);
}

export const roleLabels: Record<Role, string> = {
  admin: "Admin",
  editor: "Editor",
  viewer: "Viewer",
};

export const roleHelp: Record<Role, string> = {
  viewer: "Reads runs, tasks, schedules and workers.",
  editor: "Also cancels runs and edits schedules.",
  admin: "Also manages members and API keys.",
};

export const addMemberSchema = z.object({
  username: z.string().trim().min(1, "Enter a username"),
  role: z.enum(["admin", "editor", "viewer"]),
});

export type AddMemberValues = z.infer<typeof addMemberSchema>;
