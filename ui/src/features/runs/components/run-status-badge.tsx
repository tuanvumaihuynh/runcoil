import { Badge } from "@/components/ui/badge";
import { runStatusLabels, runStatusVariants } from "@/features/runs/status";
import type { RunStatus } from "@/features/runs/status";

type RunStatusBadgeProps = {
  status: RunStatus;
};

export function RunStatusBadge({ status }: RunStatusBadgeProps) {
  return <Badge variant={runStatusVariants[status]}>{runStatusLabels[status]}</Badge>;
}
