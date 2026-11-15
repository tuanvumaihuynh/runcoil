import { z } from "zod";

import type { components } from "@/api/schema";
import { passwordSchema } from "@/features/auth/schemas";

type UpdateUserRequest = components["schemas"]["UpdateUserRequest"];

const optionalEmailSchema = z
  .string()
  .trim()
  .pipe(z.union([z.literal(""), z.email("Enter a valid email")]));

export const createUserSchema = z.object({
  username: z.string().trim().min(1, "Enter a username"),
  email: optionalEmailSchema,
  password: passwordSchema,
  isServerAdmin: z.boolean(),
});

export type CreateUserValues = z.infer<typeof createUserSchema>;

export const editUserSchema = z.object({
  email: optionalEmailSchema,
  isServerAdmin: z.boolean(),
  disabled: z.boolean(),
});

export type EditUserValues = z.infer<typeof editUserSchema>;

export function updateUserBody(values: EditUserValues): UpdateUserRequest {
  const email = values.email.trim();

  return {
    email: email === "" ? null : email,
    isServerAdmin: values.isServerAdmin,
    disabled: values.disabled,
  };
}

export const resetPasswordSchema = z
  .object({
    newPassword: passwordSchema,
    confirmPassword: z.string(),
  })
  .refine((values) => values.newPassword === values.confirmPassword, {
    message: "The passwords do not match",
    path: ["confirmPassword"],
  });

export type ResetPasswordValues = z.infer<typeof resetPasswordSchema>;
