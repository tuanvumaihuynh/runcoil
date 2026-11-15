import { toast } from "sonner";

import type { components } from "@/api/schema";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { errorMessage } from "@/lib/error-message";
import { useRemoveMember } from "@/features/members/queries";

type Member = components["schemas"]["Member"];

type RemoveMemberDialogProps = {
  projectId: string;
  projectName: string;
  member: Member;
  onClose: () => void;
};

// Mounted per member. The action keeps the dialog open until the request ends.
export function RemoveMemberDialog({
  projectId,
  projectName,
  member,
  onClose,
}: RemoveMemberDialogProps) {
  const removeMember = useRemoveMember();

  function remove(event: React.MouseEvent<HTMLButtonElement>) {
    event.preventDefault();
    removeMember.mutate(
      { params: { path: { projectId, userId: member.userId } } },
      {
        onSuccess: () => {
          toast.success("Member removed");
          onClose();
        },
        onError: (error) => toast.error(errorMessage(error, "Could not remove the member.")),
      },
    );
  }

  return (
    <AlertDialog open onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            Remove {member.username} from {projectName}?
          </AlertDialogTitle>
          <AlertDialogDescription>They lose access to this project.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={remove}
            disabled={removeMember.isPending}
          >
            Remove
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
