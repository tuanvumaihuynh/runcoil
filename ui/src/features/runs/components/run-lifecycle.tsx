import { CircleCheck, CircleDot, CircleX } from "lucide-react";

import type { LifecycleEntry } from "@/features/runs/lifecycle";

const icons: Record<LifecycleEntry["state"], { Icon: typeof CircleCheck; className: string }> = {
  done: { Icon: CircleCheck, className: "text-muted-foreground" },
  current: { Icon: CircleDot, className: "text-foreground" },
  failed: { Icon: CircleX, className: "text-destructive" },
};

type RunLifecycleProps = {
  entries: LifecycleEntry[];
};

export function RunLifecycle({ entries }: RunLifecycleProps) {
  return (
    <ol className="flex flex-col gap-3" aria-label="Lifecycle">
      {entries.map((entry, index) => {
        const { Icon, className } = icons[entry.state];

        return (
          <li
            key={index}
            className="flex items-start gap-3 text-sm"
            aria-current={entry.state === "current" ? "step" : undefined}
          >
            <Icon aria-hidden className={`mt-0.5 size-4 shrink-0 ${className}`} />
            <div className="flex min-w-0 flex-col gap-0.5">
              <span className={entry.state === "current" ? "font-medium" : undefined}>
                {entry.title}
              </span>
              {entry.detail && <span className="text-muted-foreground">{entry.detail}</span>}
            </div>
          </li>
        );
      })}
    </ol>
  );
}
