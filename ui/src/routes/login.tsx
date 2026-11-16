import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { LoginForm } from "@/features/auth/components/login-form";
import { meQueryOptions } from "@/features/auth/queries";
import { safeRedirect } from "@/lib/redirect";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { z } from "zod";

const loginSearchSchema = z.object({
  redirect: z.string().optional(),
});

export const Route = createFileRoute("/login")({
  validateSearch: loginSearchSchema,
  beforeLoad: async ({ context, search }) => {
    try {
      await context.queryClient.ensureQueryData(meQueryOptions);
    } catch {
      return;
    }

    throw redirect({ href: safeRedirect(search.redirect) });
  },
  component: LoginPage,
});

function LoginPage() {
  const search = Route.useSearch();

  return (
    <main className="grid min-h-svh place-items-center bg-background p-6">
      <Card className="w-full max-w-sm">
        <CardHeader className="items-center">
          <img
            src="/runcoil-lockup-on-light.svg"
            alt="Runcoil"
            className="h-8 w-auto dark:hidden"
          />
          <img
            src="/runcoil-lockup-on-dark.svg"
            alt="Runcoil"
            className="hidden h-8 w-auto dark:block"
          />
        </CardHeader>
        <CardContent>
          <LoginForm redirectTo={safeRedirect(search.redirect)} />
        </CardContent>
      </Card>
    </main>
  );
}
