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

// The project to open when none is named: the last one opened if the user can still open it,
// else the first one.
export function pickLandingProject<T extends { id: string }>(
  projects: readonly T[],
  lastProjectId: string | undefined,
): T | undefined {
  return projects.find((project) => project.id === lastProjectId) ?? projects[0];
}
