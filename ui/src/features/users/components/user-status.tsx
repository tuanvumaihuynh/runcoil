import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { formatDate } from "@/lib/format";

type UserStatusProps = {
  /** When the user was disabled; undefined means the user is active. */
  disabledAt?: string;
};

export function UserStatus({ disabledAt }: UserStatusProps) {
  const disabled = disabledAt !== undefined;
  const status = (
    <span className="inline-flex items-center gap-2 text-sm" tabIndex={disabled ? 0 : undefined}>
      <span
        aria-hidden="true"
        className={cn("size-1.5 rounded-full", disabled ? "bg-destructive" : "bg-primary")}
      />
      {disabled ? "Disabled" : "Active"}
    </span>
  );

  if (!disabled) {
    return status;
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>{status}</TooltipTrigger>
      <TooltipContent>Disabled on {formatDate(disabledAt)}</TooltipContent>
    </Tooltip>
  );
}
