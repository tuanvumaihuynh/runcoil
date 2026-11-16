import { useCallback, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import type { components } from "@/api/schema";
import { DataTable } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { AddMemberDialog } from "@/features/members/components/add-member-dialog";
import { membersColumns } from "@/features/members/components/members-columns";
import { RemoveMemberDialog } from "@/features/members/components/remove-member-dialog";
import { useMembers, useUpdateMember } from "@/features/members/queries";
import type { Role } from "@/features/members/schemas";
import { settingsSections } from "@/features/settings/sections";
import { errorMessage } from "@/lib/error-message";
import { hasPermission } from "@/lib/permissions";

type ProjectAccess = components["schemas"]["ProjectAccess"];
type Member = components["schemas"]["Member"];

type MembersPageProps = {
  project: ProjectAccess;
};

export function MembersPage({ project }: MembersPageProps) {
  const canWrite = hasPermission(project.permissions, "members:write");
  const members = useMembers(project.id);
  const { mutateAsync: updateRole } = useUpdateMember();
  // The Remove button comes along, so the dialog can give focus back to it.
  const [removing, setRemoving] = useState<{ member: Member; trigger: HTMLElement }>();
  // Updates run side by side for different members, so track each one. The select of a member
  // with a running update stays disabled, which keeps two picks from racing each other.
  const [updatingUserIds, setUpdatingUserIds] = useState<ReadonlySet<string>>(new Set());
  const addButtonRef = useRef<HTMLButtonElement>(null);

  const changeRole = useCallback(
    async (member: Member, role: Role) => {
      setUpdatingUserIds((ids) => new Set(ids).add(member.userId));
      try {
        await updateRole({
          params: { path: { projectId: project.id, userId: member.userId } },
          body: { role },
        });
        toast.success("Role updated");
      } catch (error) {
        // The mutation put the old role back in the list.
        toast.error(errorMessage(error, "Could not update the role."));
      } finally {
        setUpdatingUserIds((ids) => {
          const next = new Set(ids);
          next.delete(member.userId);
          return next;
        });
      }
    },
    [project.id, updateRole],
  );

  const rows = useMemo(() => members.data?.members ?? [], [members.data]);
  const columns = useMemo(
    () =>
      membersColumns({
        canWrite,
        updatingUserIds,
        onRoleChange: (member, role) => void changeRole(member, role),
        onRemove: (member, trigger) => setRemoving({ member, trigger }),
      }),
    [canWrite, updatingUserIds, changeRole],
  );

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader
        title={settingsSections.members.title}
        description={settingsSections.members.description}
        actions={canWrite && <AddMemberDialog projectId={project.id} triggerRef={addButtonRef} />}
      />

      {members.isError ? (
        <div className="flex flex-col items-start gap-3">
          <p role="alert" className="text-sm text-destructive">
            Could not load members.
          </p>
          <Button variant="outline" onClick={() => void members.refetch()}>
            Try again
          </Button>
        </div>
      ) : (
        <DataTable
          columns={columns}
          data={rows}
          loading={members.isPending}
          emptyMessage="No members yet."
        />
      )}

      {removing && (
        <RemoveMemberDialog
          key={removing.member.userId}
          projectId={project.id}
          projectName={project.name}
          member={removing.member}
          trigger={removing.trigger}
          fallbackFocusRef={addButtonRef}
          onClose={() => setRemoving(undefined)}
        />
      )}
    </div>
  );
}
