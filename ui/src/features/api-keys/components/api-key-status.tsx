import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { formatDate } from "@/lib/format";

type ApiKeyStatusProps = {
  /** When the key was revoked; undefined means the key is active. */
  revokedAt?: string;
};

export function ApiKeyStatus({ revokedAt }: ApiKeyStatusProps) {
  const revoked = revokedAt !== undefined;
  const status = (
    <span className="inline-flex items-center gap-2 text-sm" tabIndex={revoked ? 0 : undefined}>
      <span
        aria-hidden="true"
        className={cn("size-1.5 rounded-full", revoked ? "bg-destructive" : "bg-primary")}
      />
      {revoked ? "Revoked" : "Active"}
    </span>
  );

  if (!revoked) {
    return status;
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>{status}</TooltipTrigger>
      <TooltipContent>Revoked on {formatDate(revokedAt)}</TooltipContent>
    </Tooltip>
  );
}
