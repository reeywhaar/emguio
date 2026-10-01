import { useSyncExternalStore } from "react";
import { useQueryClient, type QueryKey } from "@tanstack/react-query";

/**
 * What a query already holds, read without asking for it: another component owns the query,
 * and this one only draws from it, following it as it changes.
 */
export function useCached<T>(queryKey: QueryKey): T | undefined {
  const client = useQueryClient();
  return useSyncExternalStore(
    (changed) => client.getQueryCache().subscribe(changed),
    () => client.getQueryData<T>(queryKey),
  );
}
