import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { setServerErrors } from "@/lib/form-errors";
import { RoleSelect } from "@/features/members/components/role-select";
import { UserPicker } from "@/features/members/components/user-picker";
import { useAddMember } from "@/features/members/queries";
import { addMemberSchema, type AddMemberValues } from "@/features/members/schemas";

const defaultValues: AddMemberValues = { username: "", role: "viewer" };

type AddMemberDialogProps = {
  projectId: string;
};

export function AddMemberDialog({ projectId }: AddMemberDialogProps) {
  const [open, setOpen] = useState(false);
  const addMember = useAddMember();

  const form = useForm<AddMemberValues>({
    resolver: zodResolver(addMemberSchema),
    defaultValues,
  });

  function handleOpenChange(next: boolean) {
    setOpen(next);
    if (!next) {
      form.reset(defaultValues);
    }
  }

  function onSubmit({ username, role }: AddMemberValues) {
    addMember.mutate(
      { params: { path: { projectId } }, body: { username, role } },
      {
        onSuccess: () => {
          toast.success("Member added");
          handleOpenChange(false);
        },
        onError: (error) =>
          setServerErrors(
            form.setError,
            error,
            ["username"],
            "Could not add the member. Try again.",
          ),
      },
    );
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>
        <Button>Add member</Button>
      </DialogTrigger>
      <DialogContent>
        <form onSubmit={form.handleSubmit(onSubmit)} noValidate>
          <DialogHeader>
            <DialogTitle>Add member</DialogTitle>
            <DialogDescription className="sr-only">Add member</DialogDescription>
          </DialogHeader>
          <FieldGroup className="py-4">
            <Controller
              name="username"
              control={form.control}
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid}>
                  <FieldLabel htmlFor={field.name}>Username</FieldLabel>
                  <UserPicker
                    projectId={projectId}
                    id={field.name}
                    value={field.value}
                    onChange={field.onChange}
                    onBlur={field.onBlur}
                    aria-invalid={fieldState.invalid}
                  />
                  {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                </Field>
              )}
            />
            <Controller
              name="role"
              control={form.control}
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid}>
                  <FieldLabel htmlFor={field.name}>Role</FieldLabel>
                  <RoleSelect
                    id={field.name}
                    value={field.value}
                    onValueChange={field.onChange}
                    aria-invalid={fieldState.invalid}
                  />
                  {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                </Field>
              )}
            />
            {form.formState.errors.root && <FieldError errors={[form.formState.errors.root]} />}
          </FieldGroup>
          <DialogFooter>
            <Button type="submit" disabled={addMember.isPending}>
              Add member
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
