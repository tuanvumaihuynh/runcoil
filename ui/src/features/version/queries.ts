import { api } from "@/api/client";

// The server version never changes while the page is open.
export function useServerVersion() {
  return api.useQuery("get", "/version", {}, { staleTime: Infinity });
}
