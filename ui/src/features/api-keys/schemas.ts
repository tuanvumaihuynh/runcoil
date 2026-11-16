import { z } from "zod";

export const createApiKeySchema = z.object({
  name: z.string().trim().min(1, "Enter a name").max(64, "Use at most 64 characters"),
});

export type CreateApiKeyValues = z.infer<typeof createApiKeySchema>;

// Only the prefix and the last 4 characters of a key are known after it is made.
export function maskedKey(hint: string): string {
  return `rc_api_…${hint}`;
}
