import { zodResolver } from "@hookform/resolvers/zod";
import { Controller, useForm } from "react-hook-form";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { setServerErrors } from "@/lib/form-errors";
import { useChangePassword } from "@/features/auth/queries";
import { changePasswordSchema, type ChangePasswordValues } from "@/features/auth/schemas";

type ChangePasswordFormProps = {
  onSuccess?: () => void;
};

export function ChangePasswordForm({ onSuccess }: ChangePasswordFormProps) {
  const changePassword = useChangePassword();

  const form = useForm<ChangePasswordValues>({
    resolver: zodResolver(changePasswordSchema),
    defaultValues: { currentPassword: "", newPassword: "", confirmPassword: "" },
  });

  function onSubmit({ currentPassword, newPassword }: ChangePasswordValues) {
    changePassword.mutate(
      { body: { currentPassword, newPassword } },
      {
        onSuccess: () => {
          toast.success("Password changed");
          form.reset();
          onSuccess?.();
        },
        onError: (error) =>
          setServerErrors(
            form.setError,
            error,
            ["currentPassword", "newPassword"],
            "Could not change the password. Try again.",
          ),
      },
    );
  }

  return (
    <form onSubmit={form.handleSubmit(onSubmit)} noValidate>
      <FieldGroup>
        <Controller
          name="currentPassword"
          control={form.control}
          render={({ field, fieldState }) => (
            <Field data-invalid={fieldState.invalid}>
              <FieldLabel htmlFor={field.name}>Current password</FieldLabel>
              <Input
                {...field}
                id={field.name}
                type="password"
                autoComplete="current-password"
                aria-invalid={fieldState.invalid}
              />
              {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
            </Field>
          )}
        />
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
        <Button
          type="submit"
          className="w-full sm:w-auto sm:self-start"
          disabled={changePassword.isPending}
        >
          Change password
        </Button>
      </FieldGroup>
    </form>
  );
}
