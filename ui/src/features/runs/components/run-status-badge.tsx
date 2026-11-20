import type { ReactNode } from "react";

import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { runStatusHints, runStatusLabels, runStatusVariants } from "@/features/runs/status";
import type { RunStatus } from "@/features/runs/status";

type RunStatusTooltipProps = {
  status: RunStatus;
  /** The focusable element that opens the hint, such as a link around the badge. */
  children: ReactNode;
};

export function RunStatusTooltip({ status, children }: RunStatusTooltipProps) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent style={{ animation: "none" }}>{runStatusHints[status]}</TooltipContent>
    </Tooltip>
  );
}

export function RunStatusBadge({ status }: { status: RunStatus }) {
  return (
    <RunStatusTooltip status={status}>
      {/* A tab stop of its own, so the hint opens from the keyboard too. */}
      <Badge variant={runStatusVariants[status]} tabIndex={0}>
        {runStatusLabels[status]}
      </Badge>
    </RunStatusTooltip>
  );
}
