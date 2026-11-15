import { z } from "zod";

export const createProjectSchema = z.object({
  name: z.string().trim().min(1, "Enter a project name").max(64, "Use at most 64 characters"),
});

export type CreateProjectValues = z.infer<typeof createProjectSchema>;
