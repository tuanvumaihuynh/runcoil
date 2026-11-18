import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { runStatusHints, runStatusLabels, runStatusVariants } from "@/features/runs/status";
import type { RunStatus } from "@/features/runs/status";

type RunStatusBadgeProps = {
  status: RunStatus;
  /** Set when a link wraps the badge, which takes the focus instead. */
  inLink?: boolean;
};

export function RunStatusBadge({ status, inLink = false }: RunStatusBadgeProps) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        {/* A tab stop of its own, so the hint opens from the keyboard too. */}
        <Badge variant={runStatusVariants[status]} tabIndex={inLink ? undefined : 0}>
          {runStatusLabels[status]}
        </Badge>
      </TooltipTrigger>
      <TooltipContent style={{ animation: "none" }}>{runStatusHints[status]}</TooltipContent>
    </Tooltip>
  );
}
