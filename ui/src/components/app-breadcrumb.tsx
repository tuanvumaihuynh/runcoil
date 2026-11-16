import { Link, useMatches } from "@tanstack/react-router";
import { Fragment, useMemo } from "react";

import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { cn } from "@/lib/utils";

type Crumb = { id: string; label: string; pathname: string; projectId?: string };

// The project route has no page of its own: its crumb is the project name, linking to Runs.
const projectRouteId = "/_app/projects/$projectId";

export function AppBreadcrumb() {
  const matches = useMatches();

  const crumbs = useMemo<Crumb[]>(
    () =>
      matches.flatMap((match): Crumb[] => {
        if (match.routeId === projectRouteId) {
          return [
            {
              id: match.id,
              label: match.context.project.name,
              pathname: match.pathname,
              projectId: match.params.projectId,
            },
          ];
        }

        if (match.staticData.crumb) {
          return [{ id: match.id, label: match.staticData.crumb, pathname: match.pathname }];
        }

        return [];
      }),
    [matches],
  );

  if (crumbs.length === 0) {
    return null;
  }

  return (
    <Breadcrumb className="min-w-0">
      <BreadcrumbList className="min-w-0 flex-nowrap">
        {crumbs.map((crumb, index) => {
          const isLast = index === crumbs.length - 1;

          return (
            <Fragment key={crumb.id}>
              {/* Below md only the last crumb shows, so every separator hides with its neighbours. */}
              {index > 0 && <BreadcrumbSeparator className="hidden md:block" />}
              <BreadcrumbItem className={cn("min-w-0", !isLast && "hidden md:inline-flex")}>
                {isLast && <BreadcrumbPage className="truncate">{crumb.label}</BreadcrumbPage>}
                {!isLast && crumb.projectId !== undefined && (
                  <BreadcrumbLink asChild>
                    <Link to="/projects/$projectId/runs" params={{ projectId: crumb.projectId }}>
                      {crumb.label}
                    </Link>
                  </BreadcrumbLink>
                )}
                {!isLast && crumb.projectId === undefined && (
                  <BreadcrumbLink asChild>
                    <Link to={crumb.pathname}>{crumb.label}</Link>
                  </BreadcrumbLink>
                )}
              </BreadcrumbItem>
            </Fragment>
          );
        })}
      </BreadcrumbList>
    </Breadcrumb>
  );
}
