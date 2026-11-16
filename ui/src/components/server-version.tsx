import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useServerVersion } from "@/features/version/queries";
import { formatDate } from "@/lib/format";

// Shows the server version. It renders nothing while loading or when the request fails.
export function ServerVersion() {
  const { data } = useServerVersion();

  if (data === undefined) {
    return null;
  }

  const details = [data.commit.slice(0, 7), data.buildDate ? formatDate(data.buildDate) : ""]
    .filter((part) => part !== "")
    .join(" · ");
  const label = (
    <span className="block truncate px-2 text-xs whitespace-nowrap text-muted-foreground">
      Runcoil {data.version}
    </span>
  );

  return (
    <div className="h-4 overflow-hidden transition-[margin,opacity] duration-200 ease-linear group-data-[collapsible=icon]:pointer-events-none group-data-[collapsible=icon]:-mt-6 group-data-[collapsible=icon]:opacity-0">
      {details === "" ? (
        label
      ) : (
        <Tooltip>
          <TooltipTrigger asChild>{label}</TooltipTrigger>
          <TooltipContent side="right">{details}</TooltipContent>
        </Tooltip>
      )}
    </div>
  );
}
