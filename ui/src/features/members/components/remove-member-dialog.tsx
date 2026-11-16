import { useSuspenseQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useRef, type RefObject } from "react";
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
import { meQueryOptions } from "@/features/auth/queries";
import { errorMessage } from "@/lib/error-message";
import { useRemoveMember } from "@/features/members/queries";

type Member = components["schemas"]["Member"];

type RemoveMemberDialogProps = {
  projectId: string;
  projectName: string;
  member: Member;
  /** The Remove button that opened the dialog. It gets focus back when the dialog closes. */
  trigger: HTMLElement;
  /** Gets focus instead when the member is gone, and so is their Remove button. */
  fallbackFocusRef: RefObject<HTMLElement | null>;
  onClose: () => void;
};

// Mounted per member. The action keeps the dialog open until the request ends.
export function RemoveMemberDialog({
  projectId,
  projectName,
  member,
  trigger,
  fallbackFocusRef,
  onClose,
}: RemoveMemberDialogProps) {
  const removeMember = useRemoveMember();
  const { data: me } = useSuspenseQuery(meQueryOptions);
  const navigate = useNavigate();
  const removedRef = useRef(false);

  function remove(event: React.MouseEvent<HTMLButtonElement>) {
    event.preventDefault();
    removeMember.mutate(
      { params: { path: { projectId, userId: member.userId } } },
      {
        onSuccess: () => {
          toast.success("Member removed");
          removedRef.current = true;
          onClose();
          if (member.userId === me.user.id) {
            // They left the project, so this page is no longer theirs.
            void navigate({ to: "/" });
          }
        },
        onError: (error) => toast.error(errorMessage(error, "Could not remove the member.")),
      },
    );
  }

  return (
    <AlertDialog open onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent
        onCloseAutoFocus={(event) => {
          // The dialog has no trigger of its own, so focus would fall to the body.
          event.preventDefault();
          (removedRef.current ? fallbackFocusRef.current : trigger)?.focus();
        }}
      >
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
