import { Link } from "@tanstack/react-router";
import { Circle, CircleCheck, CircleDot } from "lucide-react";
import type { ReactNode } from "react";

import { CodeBlock } from "@/components/code-block";
import { Button } from "@/components/ui/button";
import {
  Item,
  ItemActions,
  ItemContent,
  ItemGroup,
  ItemMedia,
  ItemTitle,
} from "@/components/ui/item";
import { Skeleton } from "@/components/ui/skeleton";
import { useApiKeys } from "@/features/api-keys/queries";
import { checklistSteps } from "@/features/get-started/checklist";
import type { ChecklistStep, ChecklistStepId } from "@/features/get-started/checklist";
import { workerCommands, workerProgram } from "@/features/get-started/worker-snippet";
import { useTasks } from "@/features/tasks/queries";
import { useWorkers } from "@/features/workers/queries";
import { hasPermission } from "@/lib/permissions";
import type { Permission } from "@/lib/permissions";

type GetStartedProps = {
  projectId: string;
  permissions: readonly Permission[] | undefined;
};

const titles: Record<ChecklistStepId, string> = {
  apiKey: "Create an API key",
  worker: "Start a Worker",
  tasks: "See your Tasks",
  run: "See your first Run",
};

// Shown while the project has no Run. Each step ticks itself from the project's lists.
export function GetStarted({ projectId, permissions }: GetStartedProps) {
  const canReadKeys = hasPermission(permissions, "api_keys:read");
  const canWriteKeys = hasPermission(permissions, "api_keys:write");
  const apiKeys = useApiKeys(projectId, { enabled: canReadKeys });
  const workers = useWorkers(projectId, "live");
  const tasks = useTasks(projectId);

  const loading = (canReadKeys && apiKeys.isPending) || workers.isPending || tasks.isPending;

  const steps = checklistSteps({
    hasApiKey: apiKeys.data?.apiKeys.some((key) => key.revokedAt === undefined) ?? false,
    hasLiveWorker: (workers.data?.pages[0]?.workers.length ?? 0) > 0,
    hasTask: (tasks.data?.pages[0]?.tasks.length ?? 0) > 0,
    hasRun: false,
  });

  function body(step: ChecklistStep): {
    description: ReactNode;
    action?: ReactNode;
    more?: ReactNode;
  } {
    switch (step.id) {
      case "apiKey":
        return {
          description:
            canWriteKeys || step.done
              ? "Your Workers use it to connect to this project."
              : "Your Workers use it to connect to this project. Ask a project admin to create one.",
          action: step.current && canWriteKeys && (
            <Button asChild size="sm">
              <Link to="/projects/$projectId/settings/api-keys" params={{ projectId }}>
                Create API key
              </Link>
            </Button>
          ),
        };
      case "worker":
        return {
          description: "A Worker is a process of yours that registers Tasks and runs them.",
          more: !step.done && (
            <div className="flex flex-col gap-3">
              <p className="text-sm text-muted-foreground">
                Save this program as <code className="font-mono">main.go</code> in a new folder:
              </p>
              <CodeBlock code={workerProgram} label="main.go" />
              <p className="text-sm text-muted-foreground">Then run it with your key:</p>
              <CodeBlock code={workerCommands(window.location)} label="the commands" />
            </div>
          ),
        };
      case "tasks":
        return {
          description:
            "A Task is a function a Worker registers. Its Tasks appear once it connects.",
          action: step.done && (
            <Button asChild size="sm" variant="outline">
              <Link to="/projects/$projectId/tasks" params={{ projectId }}>
                Open Tasks
              </Link>
            </Button>
          ),
        };
      case "run":
        return {
          description:
            "A Run is one execution of a Task. The schedule starts one within a minute, and it shows up here.",
        };
    }
  }

  return (
    <section className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-medium">Get started</h2>
        <p className="text-sm text-muted-foreground">
          Connect your code to this project and see its first Run.
        </p>
      </div>

      {loading ? (
        <Skeleton className="h-48 w-full" />
      ) : (
        <ItemGroup>
          {steps.map((step) => {
            const { description, action, more } = body(step);
            const Icon = step.done ? CircleCheck : step.current ? CircleDot : Circle;

            return (
              <Item
                key={step.id}
                role="listitem"
                variant="outline"
                aria-current={step.current ? "step" : undefined}
              >
                <ItemMedia variant="icon">
                  <Icon
                    aria-label={step.done ? "Done" : "Not done"}
                    className={step.current ? "text-foreground" : "text-muted-foreground"}
                  />
                </ItemMedia>
                <ItemContent>
                  <ItemTitle className={step.done ? "text-muted-foreground" : undefined}>
                    {titles[step.id]}
                  </ItemTitle>
                  <p className="text-sm text-muted-foreground">{description}</p>
                </ItemContent>
                {action && <ItemActions>{action}</ItemActions>}
                {more && <div className="w-full">{more}</div>}
              </Item>
            );
          })}
        </ItemGroup>
      )}
    </section>
  );
}
