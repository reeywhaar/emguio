import { useEffect } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";

import { qk } from "@app/api/keys";
import type { Mailbox } from "@app/api/types";

/**
 * Keeps an open tab in step with the mail arriving underneath it.
 *
 * The server says only that something changed, so this refetches what is on screen rather than
 * patching a cache from a payload — one round trip against a second copy of the model.
 *
 * EventSource rather than a socket: it reconnects on its own with backoff.
 */
export function useLive() {
  const client = useQueryClient();

  useEffect(() => {
    if (typeof EventSource === "undefined") return;
    const source = new EventSource("/api/events");
    // Jobs first: one the server has done is written into the lists before they are read
    // again, so the counts it moved are not moved twice in between.
    const refresh = async () => {
      await client.invalidateQueries({ queryKey: qk.jobs });
      client.invalidateQueries({ queryKey: qk.emailConfigs });
      client.invalidateQueries({
        queryKey: qk.mail,
        predicate: ({ queryKey }) => kept(client, queryKey),
      });
    };
    source.addEventListener("changed", refresh);
    // A reconnection means the stream was down, and whatever happened meanwhile was not said.
    // Asking once on the way back is cheaper than tracking what was missed.
    source.addEventListener("open", refresh);
    return () => source.close();
  }, [client]);
}

/**
 * Whether a query reads what the server keeps, which is what the stream announces: each
 * config's mailboxes, and INBOX's list. Every other list and every message is fetched from the
 * mail server, and asking again on each change would be a trip there every time.
 */
export function kept(client: QueryClient, key: readonly unknown[]): boolean {
  const [, config, kind, mailbox] = key;
  if (kind === "mailboxes") return true;
  if (kind !== "messages" || typeof config !== "string") return false;
  return (
    client
      .getQueryData<Mailbox[]>(qk.mailboxes(config))
      ?.some((mb) => mb.id === mailbox && mb.special_use === "inbox") ?? false
  );
}
