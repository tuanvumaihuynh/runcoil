import { z } from "zod";

import type { components } from "@/api/schema";

export type Role = components["schemas"]["Role"];

export const roles = ["viewer", "editor", "admin"] as const satisfies readonly Role[];

export function isRole(value: string): value is Role {
  return (roles as readonly string[]).includes(value);
}

export const roleLabels: Record<Role, string> = {
  admin: "Admin",
  editor: "Editor",
  viewer: "Viewer",
};

export const roleHelp: Record<Role, string> = {
  viewer: "Can view runs, tasks, schedules and workers.",
  editor: "Can also cancel runs and pause schedules.",
  admin: "Can also manage members and API keys.",
};

export const addMemberSchema = z.object({
  username: z.string().trim().min(1, "Select a user"),
  role: z.enum(roles),
});

export type AddMemberValues = z.infer<typeof addMemberSchema>;
