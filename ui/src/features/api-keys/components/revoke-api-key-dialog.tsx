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
import { useRevokeApiKey } from "@/features/api-keys/queries";
import { errorMessage } from "@/lib/error-message";

type ApiKey = components["schemas"]["ApiKey"];

type RevokeApiKeyDialogProps = {
  projectId: string;
  apiKey: ApiKey;
  /** The Revoke button that opened the dialog. It gets focus back when the dialog closes. */
  trigger: HTMLElement;
  /** Gets focus instead when the key is revoked, and so its Revoke button is gone. */
  fallbackFocusRef: RefObject<HTMLElement | null>;
  onClose: () => void;
};

// Mounted per key. The action keeps the dialog open until the request ends.
export function RevokeApiKeyDialog({
  projectId,
  apiKey,
  trigger,
  fallbackFocusRef,
  onClose,
}: RevokeApiKeyDialogProps) {
  const revokeApiKey = useRevokeApiKey();
  const revokedRef = useRef(false);

  function revoke(event: React.MouseEvent<HTMLButtonElement>) {
    event.preventDefault();
    revokeApiKey.mutate(
      { params: { path: { projectId, apiKeyId: apiKey.id } } },
      {
        onSuccess: () => {
          toast.success("API key revoked");
          revokedRef.current = true;
          onClose();
        },
        onError: (error) => toast.error(errorMessage(error, "Could not revoke the API key.")),
      },
    );
  }

  return (
    <AlertDialog open onOpenChange={(open) => !open && !revokeApiKey.isPending && onClose()}>
      <AlertDialogContent
        // AlertDialog has no close button and ignores outside clicks, so Escape is the only other
        // way out. dismissGuard also sets a prop AlertDialogContent does not take, so the
        // equivalent is inline.
        onEscapeKeyDown={(event) => {
          if (revokeApiKey.isPending) {
            event.preventDefault();
          }
        }}
        onCloseAutoFocus={(event) => {
          // The dialog has no trigger of its own, so focus would fall to the body.
          event.preventDefault();
          (revokedRef.current ? fallbackFocusRef.current : trigger)?.focus();
        }}
      >
        <AlertDialogHeader>
          <AlertDialogTitle>Revoke {apiKey.name}?</AlertDialogTitle>
          <AlertDialogDescription>
            Workers using this key are disconnected and cannot connect with it again.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={revokeApiKey.isPending}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={revoke}
            disabled={revokeApiKey.isPending}
          >
            Revoke key
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
