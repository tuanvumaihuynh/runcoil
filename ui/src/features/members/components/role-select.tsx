import type { ComponentProps } from "react";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { isRole, roleHelp, roleLabels, roles, type Role } from "@/features/members/schemas";

type RoleSelectProps = {
  value: Role;
  onValueChange: (role: Role) => void;
} & Pick<
  ComponentProps<typeof SelectTrigger>,
  "id" | "size" | "className" | "aria-label" | "aria-invalid"
>;

export function RoleSelect({ value, onValueChange, ...triggerProps }: RoleSelectProps) {
  return (
    <Select
      value={value}
      onValueChange={(next) => {
        if (isRole(next)) {
          onValueChange(next);
        }
      }}
    >
      <SelectTrigger {...triggerProps}>
        <SelectValue>{roleLabels[value]}</SelectValue>
      </SelectTrigger>
      <SelectContent position="popper" align="start" className="min-w-64">
        {roles.map((role) => (
          <SelectItem key={role} value={role}>
            <span className="flex flex-col items-start gap-0.5">
              <span className="font-medium">{roleLabels[role]}</span>
              <span className="text-xs text-muted-foreground">{roleHelp[role]}</span>
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
