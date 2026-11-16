import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";

import { PageHeader } from "@/components/page-header";
import { SettingsRow } from "@/components/settings-row";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ChangePasswordForm } from "@/features/auth/components/change-password-form";
import { meQueryOptions } from "@/features/auth/queries";
import { scrollableDialogClass } from "@/lib/dialog";
import { isThemePreference, useTheme } from "@/lib/theme";

export const Route = createFileRoute("/_app/account")({
  staticData: { crumb: "Account" },
  component: AccountPage,
});

function AccountPage() {
  const { data: me } = useSuspenseQuery(meQueryOptions);
  const { preference, setPreference } = useTheme();
  const [passwordOpen, setPasswordOpen] = useState(false);

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader title="Account" description="Manage your sign-in details." />

      <div className="flex flex-col">
        <SettingsRow label="Username" description="Set by a server admin.">
          <span className="font-mono">{me.user.username}</span>
        </SettingsRow>
        <SettingsRow label="Email" description="You can also sign in with it.">
          {me.user.email ?? <span className="text-muted-foreground">No email</span>}
        </SettingsRow>
        <SettingsRow label="Password" description="Changing it signs you out everywhere else.">
          <Dialog open={passwordOpen} onOpenChange={setPasswordOpen}>
            <DialogTrigger asChild>
              <Button variant="outline">Change password</Button>
            </DialogTrigger>
            <DialogContent className={scrollableDialogClass}>
              <DialogHeader>
                <DialogTitle>Change password</DialogTitle>
              </DialogHeader>
              <ChangePasswordForm onSuccess={() => setPasswordOpen(false)} />
            </DialogContent>
          </Dialog>
        </SettingsRow>
        <SettingsRow label="Theme">
          <Select
            value={preference}
            onValueChange={(value) => {
              if (isThemePreference(value)) {
                setPreference(value);
              }
            }}
          >
            <SelectTrigger className="w-32" aria-label="Theme">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="light">Light</SelectItem>
              <SelectItem value="dark">Dark</SelectItem>
              <SelectItem value="system">System</SelectItem>
            </SelectContent>
          </Select>
        </SettingsRow>
      </div>
    </div>
  );
}
