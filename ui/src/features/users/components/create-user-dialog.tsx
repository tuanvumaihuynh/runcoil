import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
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
import { useCreateUser } from "@/features/users/queries";
import { createUserSchema, type CreateUserValues } from "@/features/users/schemas";

const defaultValues: CreateUserValues = {
  username: "",
  email: "",
  password: "",
  isServerAdmin: false,
};

export function CreateUserDialog() {
  const [open, setOpen] = useState(false);
  const createUser = useCreateUser();

  const form = useForm<CreateUserValues>({
    resolver: zodResolver(createUserSchema),
    defaultValues,
  });

  function handleOpenChange(next: boolean) {
    setOpen(next);
    if (!next) {
      form.reset(defaultValues);
    }
  }

  function onSubmit({ username, email, password, isServerAdmin }: CreateUserValues) {
    const trimmedEmail = email.trim();
    createUser.mutate(
      {
        body: {
          username,
          email: trimmedEmail === "" ? undefined : trimmedEmail,
          password,
          isServerAdmin,
        },
      },
      {
        onSuccess: () => {
          toast.success("User created");
          handleOpenChange(false);
        },
        onError: (error) =>
          setServerErrors(
            form.setError,
            error,
            ["username", "email", "password"],
            "Could not create the user. Try again.",
          ),
      },
    );
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>
        <Button>New user</Button>
      </DialogTrigger>
      <DialogContent>
        <form onSubmit={form.handleSubmit(onSubmit)} noValidate>
          <DialogHeader>
            <DialogTitle>New user</DialogTitle>
            <DialogDescription>
              Create an account that can sign in to this server.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup className="py-4">
            <Controller
              name="username"
              control={form.control}
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid}>
                  <FieldLabel htmlFor={field.name}>Username</FieldLabel>
                  <Input
                    {...field}
                    id={field.name}
                    autoComplete="off"
                    aria-invalid={fieldState.invalid}
                  />
                  {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                </Field>
              )}
            />
            <Controller
              name="email"
              control={form.control}
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid}>
                  <FieldLabel htmlFor={field.name}>Email (optional)</FieldLabel>
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
              name="password"
              control={form.control}
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid}>
                  <FieldLabel htmlFor={field.name}>Password</FieldLabel>
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
            {form.formState.errors.root && <FieldError errors={[form.formState.errors.root]} />}
          </FieldGroup>
          <DialogFooter>
            <Button type="submit" disabled={createUser.isPending}>
              Create user
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
