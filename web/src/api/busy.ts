import { useSyncExternalStore } from "react";

/**
 * Which email configs the server is working on now, as the event stream last said: a page-wide
 * value rather than a query, since nothing is fetched for it. None while the stream is down.
 */
let busy: ReadonlySet<string> = new Set();
const listeners = new Set<() => void>();

export function setBusy(ids: string[]) {
  busy = new Set(ids);
  for (const listener of listeners) listener();
}

/** Whether the server is working on this config's mail server now. */
export function useBusy(config: string): boolean {
  return useSyncExternalStore(
    (changed) => {
      listeners.add(changed);
      return () => listeners.delete(changed);
    },
    () => busy.has(config),
  );
}
