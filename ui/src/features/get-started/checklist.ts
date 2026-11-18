export type ChecklistStepId = "apiKey" | "worker" | "tasks" | "run";

export type ChecklistStep = {
  id: ChecklistStepId;
  done: boolean;
  /** The first step not done yet. */
  current: boolean;
};

export type ProjectProgress = {
  hasApiKey: boolean;
  hasLiveWorker: boolean;
  hasTask: boolean;
  hasRun: boolean;
};

// Each step counts as done once a later one is, so a revoked key or a stopped Worker does
// not undo the steps after it.
export function checklistSteps({
  hasApiKey,
  hasLiveWorker,
  hasTask,
  hasRun,
}: ProjectProgress): ChecklistStep[] {
  const run = hasRun;
  const tasks = run || hasTask;
  const worker = tasks || hasLiveWorker;
  const apiKey = worker || hasApiKey;

  const done: [ChecklistStepId, boolean][] = [
    ["apiKey", apiKey],
    ["worker", worker],
    ["tasks", tasks],
    ["run", run],
  ];
  const current = done.find(([, isDone]) => !isDone)?.[0];

  return done.map(([id, isDone]) => ({ id, done: isDone, current: id === current }));
}
