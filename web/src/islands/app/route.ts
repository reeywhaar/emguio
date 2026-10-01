import { useMemo, useSyncExternalStore } from "react";

/**
 * Where the app is, read from the address bar and nowhere else, so a reload or a copied link
 * lands on the same thing.
 */
export type Route =
  | { page: "mail"; config: string | null; mailbox: string | null }
  | { page: "settings" }
  | { page: "new-config" }
  | { page: "edit-config"; id: string }
  | { page: "missing" };

export const paths = {
  mail: (config?: string, mailbox?: string) =>
    config
      ? `/c/${encodeURIComponent(config)}${mailbox ? `/${encodeURIComponent(mailbox)}` : ""}`
      : "/",
  settings: "/settings",
  newConfig: "/settings/email-configs/new",
  editConfig: (id: string) =>
    `/settings/email-configs/${encodeURIComponent(id)}`,
};

export function parse(path: string): Route {
  const parts = path.split("/").filter(Boolean).map(decodeURIComponent);
  const [first, second, third] = parts;
  if (parts.length === 0) return { page: "mail", config: null, mailbox: null };
  if (first === "c" && (parts.length === 2 || parts.length === 3))
    return { page: "mail", config: second!, mailbox: third ?? null };
  if (first === "settings" && parts.length === 1) return { page: "settings" };
  if (first === "settings" && second === "email-configs" && parts.length === 3)
    return third === "new"
      ? { page: "new-config" }
      : { page: "edit-config", id: third! };
  return { page: "missing" };
}

const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  window.addEventListener("popstate", listener);
  return () => {
    listeners.delete(listener);
    window.removeEventListener("popstate", listener);
  };
}

/** Moves within the island, as a link does, without loading another document. */
export function go(path: string) {
  if (path === window.location.pathname) return;
  window.history.pushState(null, "", path);
  for (const listener of listeners) listener();
}

export function useRoute(): Route {
  const path = useSyncExternalStore(subscribe, () => window.location.pathname);
  return useMemo(() => parse(path), [path]);
}
