export const lastProjectStorageKey = "runcoil-last-project";

// The last project the user opened. Storage can be blocked, so a read that fails is "none".
export function readLastProject(): string | undefined {
  try {
    return window.localStorage.getItem(lastProjectStorageKey) ?? undefined;
  } catch {
    return undefined;
  }
}

export function storeLastProject(projectId: string): void {
  try {
    window.localStorage.setItem(lastProjectStorageKey, projectId);
  } catch {
    // Ignored on purpose: the sidebar falls back to the first project.
  }
}
