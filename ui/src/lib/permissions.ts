import type { components } from "@/api/schema";

export type Permission = components["schemas"]["Permission"];

export function hasPermission(
  permissions: readonly Permission[] | undefined,
  permission: Permission,
): boolean {
  return permissions?.includes(permission) ?? false;
}
