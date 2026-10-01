import { useSyncExternalStore } from "react";

/**
 * One sentence for the whole island, for what went wrong after the place it happened has gone:
 * a message moved away from the pane that moved it, and the server then refused.
 */
export type Notice = { id: number; text: string };

let current: Notice | null = null;
let next = 1;
const listeners = new Set<() => void>();

function publish(notice: Notice | null) {
  current = notice;
  for (const listener of listeners) listener();
}

export function say(text: string) {
  publish({ id: next++, text });
}

export function dismiss(id: number) {
  if (current?.id === id) publish(null);
}

export function useNotice(): Notice | null {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    () => current,
  );
}
