import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  runStatusHints,
  runStatusLabels,
  runStatusVariants,
  stepStatusHints,
} from "@/features/runs/status";
import type { RunStatus, StepStatus } from "@/features/runs/status";

type RunStatusBadgeProps =
  | { status: RunStatus; step?: false }
  | {
      status: StepStatus;
      /** Explains the status as a Step's, not a Run's. */
      step: true;
    };

export function RunStatusBadge(props: RunStatusBadgeProps) {
  const hint = props.step ? stepStatusHints[props.status] : runStatusHints[props.status];

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge variant={runStatusVariants[props.status]}>{runStatusLabels[props.status]}</Badge>
      </TooltipTrigger>
      <TooltipContent style={{ animation: "none" }}>{hint}</TooltipContent>
    </Tooltip>
  );
}
