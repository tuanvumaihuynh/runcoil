import { ChevronDown } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { runStatuses, runStatusLabels } from "@/features/runs/status";
import type { RunStatus } from "@/features/runs/status";

type RunStatusFilterProps = {
  value: RunStatus[] | undefined;
  onChange: (value: RunStatus[] | undefined) => void;
};

export function RunStatusFilter({ value, onChange }: RunStatusFilterProps) {
  const checked = runStatuses.filter((status) => value?.includes(status));
  const label = checked.length === 0 ? "Any" : checked.map((s) => runStatusLabels[s]).join(", ");

  function toggle(status: RunStatus, on: boolean) {
    const next = runStatuses.filter((item) => (item === status ? on : checked.includes(item)));
    onChange(next.length === 0 ? undefined : next);
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm">
          Status: {label}
          <ChevronDown />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        {runStatuses.map((status) => (
          <DropdownMenuCheckboxItem
            key={status}
            checked={checked.includes(status)}
            onCheckedChange={(on) => toggle(status, on)}
            onSelect={(event) => event.preventDefault()}
          >
            {runStatusLabels[status]}
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
