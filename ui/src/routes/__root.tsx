import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";

import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import { NotFound } from "@/components/not-found";
import { FullPageRouteError } from "@/components/route-error";
import { useTheme } from "@/lib/theme";

export type RouterContext = {
  queryClient: QueryClient;
};

export const Route = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
  notFoundComponent: RootNotFound,
  errorComponent: FullPageRouteError,
});

function RootLayout() {
  const { resolved } = useTheme();

  return (
    <TooltipProvider>
      <Outlet />
      <Toaster theme={resolved} />
    </TooltipProvider>
  );
}

function RootNotFound() {
  return (
    <main className="grid min-h-svh place-items-center p-6">
      <NotFound />
    </main>
  );
}
