import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";

import { Separator } from "@/components/ui/separator";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { AppBreadcrumb } from "@/components/app-breadcrumb";
import { AppSidebar } from "@/components/app-sidebar";
import { NotFound } from "@/components/not-found";
import { FullPageRouteError } from "@/components/route-error";
import { UserMenu } from "@/components/user-menu";
import { meQueryOptions } from "@/features/auth/queries";
import { isUnauthenticatedError } from "@/lib/api-errors";

export const Route = createFileRoute("/_app")({
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(meQueryOptions);
    } catch (error) {
      if (isUnauthenticatedError(error)) {
        throw redirect({ to: "/login", search: { redirect: location.href } });
      }

      throw error;
    }
  },
  component: AppLayout,
  notFoundComponent: NotFound,
  errorComponent: FullPageRouteError,
});

function AppLayout() {
  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>
        <header className="flex h-12 items-center gap-2 border-b px-4">
          <SidebarTrigger className="-ml-1" />
          <Separator orientation="vertical" className="mr-2 h-4 data-vertical:self-auto" />
          <AppBreadcrumb />
          <div className="ml-auto shrink-0">
            <UserMenu />
          </div>
        </header>
        <div className="flex flex-1 flex-col p-6">
          <Outlet />
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}
