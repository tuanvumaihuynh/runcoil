import { zodResolver } from "@hookform/resolvers/zod";
import { Controller, useForm } from "react-hook-form";
import { toast } from "sonner";

import type { components } from "@/api/schema";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { setServerErrors } from "@/lib/form-errors";
import { useUpdateUser } from "@/features/users/queries";
import { editUserSchema, updateUserBody, type EditUserValues } from "@/features/users/schemas";

type ManagedUser = components["schemas"]["ManagedUser"];

type EditUserDialogProps = {
  user: ManagedUser;
  onClose: () => void;
};

// Mounted per user, so the form starts from that user's values.
export function EditUserDialog({ user, onClose }: EditUserDialogProps) {
  const updateUser = useUpdateUser();

  const form = useForm<EditUserValues>({
    resolver: zodResolver(editUserSchema),
    defaultValues: {
      email: user.email ?? "",
      isServerAdmin: user.isServerAdmin,
      disabled: user.disabledAt !== undefined,
    },
  });

  function onSubmit(values: EditUserValues) {
    updateUser.mutate(
      { params: { path: { userId: user.id } }, body: updateUserBody(values) },
      {
        onSuccess: () => {
          toast.success("User updated");
          onClose();
        },
        onError: (error) =>
          setServerErrors(form.setError, error, ["email"], "Could not save the user. Try again."),
      },
    );
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <form onSubmit={form.handleSubmit(onSubmit)} noValidate>
          <DialogHeader>
            <DialogTitle>Edit user</DialogTitle>
            <DialogDescription>
              <span className="font-medium text-foreground">{user.username}</span>
            </DialogDescription>
          </DialogHeader>
          <FieldGroup className="py-4">
            <Controller
              name="email"
              control={form.control}
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid}>
                  <FieldLabel htmlFor={field.name}>Email</FieldLabel>
                  <Input
                    {...field}
                    id={field.name}
                    type="email"
                    autoComplete="off"
                    aria-invalid={fieldState.invalid}
                  />
                  {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                </Field>
              )}
            />
            <Controller
              name="isServerAdmin"
              control={form.control}
              render={({ field }) => (
                <Field orientation="horizontal">
                  <Checkbox
                    id={field.name}
                    checked={field.value}
                    onCheckedChange={(checked) => field.onChange(checked === true)}
                  />
                  <FieldContent>
                    <FieldLabel htmlFor={field.name}>Server admin</FieldLabel>
                    <FieldDescription>
                      Can manage users and projects and open every project.
                    </FieldDescription>
                  </FieldContent>
                </Field>
              )}
            />
            <Controller
              name="disabled"
              control={form.control}
              render={({ field }) => (
                <Field orientation="horizontal">
                  <Checkbox
                    id={field.name}
                    checked={field.value}
                    onCheckedChange={(checked) => field.onChange(checked === true)}
                  />
                  <FieldContent>
                    <FieldLabel htmlFor={field.name}>Disabled</FieldLabel>
                    <FieldDescription>
                      A disabled user cannot sign in, and their sessions end.
                    </FieldDescription>
                  </FieldContent>
                </Field>
              )}
            />
            {form.formState.errors.root && <FieldError errors={[form.formState.errors.root]} />}
          </FieldGroup>
          <DialogFooter>
            <Button type="submit" disabled={updateUser.isPending}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
