import { errorResponse, formatErrorMessage } from "@/lib/api-errors";

// The message of an API error for people, or the fallback when the error has none.
export function errorMessage(error: unknown, fallback: string): string {
  const body = errorResponse(error);
  return body ? formatErrorMessage(body.message) : fallback;
}
