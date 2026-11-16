import { useInfiniteQuery } from "@tanstack/react-query";

const pageSize = 50;

type PagedQueryOptions<TPage> = {
  /** The API path. It starts the query key, so invalidating ["get", path] reloads the list. */
  path: string;
  fetchPage: (request: {
    pageSize: number;
    pageToken: string | undefined;
    signal: AbortSignal;
  }) => Promise<{ data?: TPage; error?: unknown }>;
  /** The path and query parameters of the request. They join the query key, so each variant has its own cache entry. */
  params?: { path?: Record<string, unknown>; query?: Record<string, unknown> };
  enabled?: boolean;
  refetchInterval?: number | false;
};

// A list endpoint read page by page.
// Not api.useInfiniteQuery: it sends pageToken=0 on the first page, which the server rejects.
export function usePagedQuery<TPage extends { nextPageToken?: string }>({
  path,
  fetchPage,
  params,
  enabled,
  refetchInterval,
}: PagedQueryOptions<TPage>) {
  return useInfiniteQuery({
    queryKey: ["get", path, { params: { ...params, query: { ...params?.query, pageSize } } }],
    queryFn: async ({ pageParam, signal }) => {
      const { data, error } = await fetchPage({ pageSize, pageToken: pageParam, signal });
      if (error || data === undefined) {
        throw error ?? new Error(`The server returned no data for ${path}`);
      }
      return data;
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextPageToken,
    enabled,
    refetchInterval,
  });
}
