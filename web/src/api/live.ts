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
      const before = counts(client);
      await client.invalidateQueries({
        queryKey: qk.mail,
        predicate: ({ queryKey }) => kept(queryKey),
      });
      // A search, which nothing is kept for, is read again from the mail server once its
      // folder's counts say the server changed what is in it, so what it found never disagrees
      // with the number beside its folder — mail another client moved into Trash, say.
      // With them, what the folder's conversations hold, and every conversation open: a reply
      // that arrived, or was filed in Sent, belongs in it.
      for (const [config, mailbox] of moved(before, counts(client))) {
        client.invalidateQueries({
          queryKey: qk.folder(config, mailbox),
          predicate: ({ queryKey }) => !kept(queryKey),
        });
        client.invalidateQueries({ queryKey: qk.threadsOf(config, mailbox) });
        client.invalidateQueries({ queryKey: qk.conversations(config) });
      }
    };
    source.addEventListener("changed", refresh);
    // A reconnection means the stream was down, and whatever happened meanwhile was not said.
    // Asking once on the way back is cheaper than tracking what was missed.
    source.addEventListener("open", refresh);
    return () => source.close();
  }, [client]);
}

/**
 * Every config's mailboxes' counts as last read, by config and mailbox, with where the next
 * message goes: a draft saved again replaces one, and the counts stay as they were.
 */
export function counts(client: QueryClient): Map<string, string> {
  const out = new Map<string, string>();
  const read = client.getQueriesData<Mailbox[]>({
    queryKey: qk.mail,
    predicate: ({ queryKey }) => queryKey[2] === "mailboxes",
  });
  for (const [key, boxes] of read) {
    for (const mb of boxes ?? []) {
      out.set(
        `${String(key[1])} ${mb.id}`,
        `${mb.messages} ${mb.unseen} ${mb.uid_next}`,
      );
    }
  }
  return out;
}

/** The mailboxes, as [config, mailbox], whose counts are not what they were. */
export function moved(
  before: Map<string, string>,
  after: Map<string, string>,
): [string, string][] {
  return [...after]
    .filter(([at, now]) => before.has(at) && before.get(at) !== now)
    .map(([at]) => at.split(" ") as [string, string]);
}

/**
 * Whether a query reads what the server keeps, which is what the stream announces: each
 * config's mailboxes, and every folder's list, which opens on the newest kept of it. A search and
 * a message are fetched from the mail server, and asking again on each change would be a trip
 * there every time.
 */
export function kept(key: readonly unknown[]): boolean {
  const [, , kind, , q] = key;
  return kind === "mailboxes" || (kind === "messages" && !q);
}
