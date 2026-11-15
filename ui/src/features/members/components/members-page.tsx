import { useMemo, useState } from "react";
import { toast } from "sonner";

import type { components } from "@/api/schema";
import { DataTable } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { AddMemberDialog } from "@/features/members/components/add-member-dialog";
import { membersColumns } from "@/features/members/components/members-columns";
import { RemoveMemberDialog } from "@/features/members/components/remove-member-dialog";
import { useMembers, useUpdateMember } from "@/features/members/queries";
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
  const { mutate: updateRole } = useUpdateMember();
  const { refetch: refetchMembers } = members;
  const [removingMember, setRemovingMember] = useState<Member>();

  const rows = useMemo(() => members.data?.members ?? [], [members.data]);
  const columns = useMemo(
    () =>
      membersColumns({
        canWrite,
        onRoleChange: (member, role) =>
          updateRole(
            { params: { path: { projectId: project.id, userId: member.userId } }, body: { role } },
            {
              onSuccess: () => toast.success("Role updated"),
              onError: (error) => {
                toast.error(errorMessage(error, "Could not update the role."));
                // Reload the list so the select shows the role the server still has.
                void refetchMembers();
              },
            },
          ),
        onRemove: (member) => setRemovingMember(member),
      }),
    [canWrite, project.id, updateRole, refetchMembers],
  );

  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader
        title="Members"
        description="Who can open this project and what they can do. Server admins can open every project and are not listed."
        actions={canWrite && <AddMemberDialog projectId={project.id} />}
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

      {removingMember && (
        <RemoveMemberDialog
          key={removingMember.userId}
          projectId={project.id}
          projectName={project.name}
          member={removingMember}
          onClose={() => setRemovingMember(undefined)}
        />
      )}
    </div>
  );
}
