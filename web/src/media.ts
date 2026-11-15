import { useSyncExternalStore } from "react";

/** Wide enough for the list and an open message side by side, as Mail lays them out: lg. */
export const SIDE_BY_SIDE = "(min-width: 64rem)";

/** Whether query matches, as the window changes. */
export function useMatches(query: string) {
  return useSyncExternalStore(
    (changed) => {
      const list = window.matchMedia(query);
      list.addEventListener("change", changed);
      return () => list.removeEventListener("change", changed);
    },
    () => window.matchMedia(query).matches,
  );
}
