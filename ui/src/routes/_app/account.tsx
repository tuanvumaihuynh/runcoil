import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ChangePasswordForm } from "@/features/auth/components/change-password-form";
import { meQueryOptions } from "@/features/auth/queries";

export const Route = createFileRoute("/_app/account")({
  staticData: { crumb: "Account" },
  component: AccountPage,
});

function AccountPage() {
  const { data: me } = useSuspenseQuery(meQueryOptions);

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-6">
      <Card>
        <CardHeader>
          <CardTitle>Account</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid gap-4 sm:grid-cols-[8rem_1fr]">
            <dt className="text-muted-foreground">Username</dt>
            <dd>{me.user.username}</dd>
            <dt className="text-muted-foreground">Email</dt>
            <dd>{me.user.email ?? "No email"}</dd>
          </dl>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Change password</CardTitle>
        </CardHeader>
        <CardContent>
          <ChangePasswordForm />
        </CardContent>
      </Card>
    </div>
  );
}
