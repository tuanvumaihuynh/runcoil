import type { ComponentProps, ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

type RowActionProps = Omit<ComponentProps<typeof Button>, "aria-label" | "size" | "variant"> & {
  label: string;
};

// Small icon button at the end of a table row: the label is both the accessible name and the tooltip.
export function RowAction({ label, children, ...props }: RowActionProps) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label={label} {...props}>
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

export function RowActions({ children }: { children: ReactNode }) {
  return <div className="flex justify-end gap-1">{children}</div>;
}
