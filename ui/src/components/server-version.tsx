import { useSidebar } from "@/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useServerVersion } from "@/features/version/queries";
import { formatDate } from "@/lib/format";
import { cn } from "@/lib/utils";

// Shows the server version. It renders nothing while loading or when the request fails.
export function ServerVersion() {
  const { data } = useServerVersion();
  const { state } = useSidebar();

  if (data === undefined) {
    return null;
  }

  const details = [data.commit.slice(0, 7), data.buildDate ? formatDate(data.buildDate) : ""]
    .filter((part) => part !== "")
    .join(" · ");
  const hasTooltip = details !== "";
  const label = (
    <span
      // Reachable by keyboard only when there is a tooltip, and not while the label is hidden.
      tabIndex={hasTooltip && state === "expanded" ? 0 : undefined}
      className={cn(
        "block truncate px-2 text-xs whitespace-nowrap text-muted-foreground",
        // Inset, because the wrapper clips what sits outside it.
        hasTooltip &&
          "rounded-sm outline-none focus-visible:ring-[3px] focus-visible:ring-inset focus-visible:ring-ring/50",
      )}
    >
      Runcoil {data.version}
    </span>
  );

  return (
    <div className="h-4 overflow-hidden transition-[margin,opacity] duration-200 ease-linear group-data-[collapsible=icon]:pointer-events-none group-data-[collapsible=icon]:-mt-6 group-data-[collapsible=icon]:opacity-0">
      {hasTooltip ? (
        <Tooltip>
          <TooltipTrigger asChild>{label}</TooltipTrigger>
          <TooltipContent side="right">{details}</TooltipContent>
        </Tooltip>
      ) : (
        label
      )}
    </div>
  );
}
