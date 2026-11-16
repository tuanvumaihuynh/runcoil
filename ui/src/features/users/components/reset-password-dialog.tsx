import { zodResolver } from "@hookform/resolvers/zod";
import { Controller, useForm } from "react-hook-form";
import { toast } from "sonner";

import type { components } from "@/api/schema";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { dismissGuard, scrollableDialogClass } from "@/lib/dialog";
import { setServerErrors } from "@/lib/form-errors";
import { useResetUserPassword } from "@/features/users/queries";
import { resetPasswordSchema, type ResetPasswordValues } from "@/features/users/schemas";

type ManagedUser = components["schemas"]["ManagedUser"];

type ResetPasswordDialogProps = {
  user: ManagedUser;
  onClose: () => void;
};

// Mounted per user, so the form starts empty for that user.
export function ResetPasswordDialog({ user, onClose }: ResetPasswordDialogProps) {
  const resetPassword = useResetUserPassword();

  const form = useForm<ResetPasswordValues>({
    resolver: zodResolver(resetPasswordSchema),
    defaultValues: { newPassword: "", confirmPassword: "" },
  });

  function onSubmit({ newPassword }: ResetPasswordValues) {
    resetPassword.mutate(
      { params: { path: { userId: user.id } }, body: { newPassword } },
      {
        onSuccess: () => {
          toast.success("Password reset");
          onClose();
        },
        onError: (error) =>
          setServerErrors(
            form.setError,
            error,
            ["newPassword"],
            "Could not reset the password. Try again.",
          ),
      },
    );
  }

  return (
    <Dialog open onOpenChange={(open) => !open && !resetPassword.isPending && onClose()}>
      <DialogContent className={scrollableDialogClass} {...dismissGuard(resetPassword.isPending)}>
        <form onSubmit={form.handleSubmit(onSubmit)} noValidate>
          <DialogHeader>
            <DialogTitle>
              Reset password for <span className="font-mono">{user.username}</span>
            </DialogTitle>
            <DialogDescription>Set a new password and sign them out everywhere.</DialogDescription>
          </DialogHeader>
          <FieldGroup className="py-4">
            <Controller
              name="newPassword"
              control={form.control}
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid}>
                  <FieldLabel htmlFor={field.name}>New password</FieldLabel>
                  <Input
                    {...field}
                    id={field.name}
                    type="password"
                    autoComplete="new-password"
                    aria-invalid={fieldState.invalid}
                  />
                  {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                </Field>
              )}
            />
            <Controller
              name="confirmPassword"
              control={form.control}
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid}>
                  <FieldLabel htmlFor={field.name}>Confirm new password</FieldLabel>
                  <Input
                    {...field}
                    id={field.name}
                    type="password"
                    autoComplete="new-password"
                    aria-invalid={fieldState.invalid}
                  />
                  {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                </Field>
              )}
            />
            {form.formState.errors.root && <FieldError errors={[form.formState.errors.root]} />}
          </FieldGroup>
          <DialogFooter>
            <Button type="submit" disabled={resetPassword.isPending}>
              Reset password
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
