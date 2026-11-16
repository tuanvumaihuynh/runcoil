import { useSuspenseQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import type { components } from "@/api/schema";
import { DataTable } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { adminSections } from "@/features/admin/sections";
import { meQueryOptions } from "@/features/auth/queries";
import { CreateUserDialog } from "@/features/users/components/create-user-dialog";
import { EditUserDialog } from "@/features/users/components/edit-user-dialog";
import { ResetPasswordDialog } from "@/features/users/components/reset-password-dialog";
import { usersColumns } from "@/features/users/components/users-columns";
import { useUsers } from "@/features/users/queries";
import { hasPermission } from "@/lib/permissions";

type ManagedUser = components["schemas"]["ManagedUser"];

export function UsersPage() {
  const { data: me } = useSuspenseQuery(meQueryOptions);
  const canWrite = hasPermission(me.permissions, "users:write");
  const users = useUsers();
  const [editingUser, setEditingUser] = useState<ManagedUser>();
  const [resettingUser, setResettingUser] = useState<ManagedUser>();

  const rows = useMemo(() => users.data?.pages.flatMap((page) => page.users) ?? [], [users.data]);
  const columns = useMemo(
    () =>
      usersColumns({
        canWrite,
        onEdit: (user) => setEditingUser(user),
        onResetPassword: (user) => setResettingUser(user),
      }),
    [canWrite],
  );

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader
        title={adminSections.users.title}
        description={adminSections.users.description}
        actions={canWrite && <CreateUserDialog />}
      />

      {users.isError ? (
        <div className="flex flex-col items-start gap-3">
          <p role="alert" className="text-sm text-destructive">
            Could not load users.
          </p>
          <Button variant="outline" onClick={() => void users.refetch()}>
            Try again
          </Button>
        </div>
      ) : (
        <DataTable
          columns={columns}
          data={rows}
          loading={users.isPending}
          emptyMessage="No users yet."
        />
      )}

      {users.hasNextPage && (
        <div>
          <Button
            variant="outline"
            onClick={() => void users.fetchNextPage()}
            disabled={users.isFetchingNextPage}
          >
            Load more
          </Button>
        </div>
      )}

      {editingUser && (
        <EditUserDialog
          key={editingUser.id}
          user={editingUser}
          onClose={() => setEditingUser(undefined)}
        />
      )}
      {resettingUser && (
        <ResetPasswordDialog
          key={resettingUser.id}
          user={resettingUser}
          onClose={() => setResettingUser(undefined)}
        />
      )}
    </div>
  );
}
