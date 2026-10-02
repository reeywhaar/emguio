import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";

import { qk } from "@app/api/keys";
import { counts, kept, moved } from "@app/api/live";
import type { Mailbox } from "@app/api/types";

const box = (id: string, special_use: Mailbox["special_use"]): Mailbox => ({
  id,
  name: id,
  path: [id],
  special_use,
  selectable: true,
  messages: 1,
  unseen: 0,
});

// What the stream announces is what the server keeps; the rest is a trip to the mail server.
describe("what a change refetches", () => {
  const client = new QueryClient();
  client.setQueryData(qk.mailboxes("ec_1"), [
    box("mb_inbox", "inbox"),
    box("mb_work", ""),
  ]);

  it("is the mailboxes and INBOX's list", () => {
    expect(kept(client, qk.mailboxes("ec_1"))).toBe(true);
    expect(kept(client, qk.messages("ec_1", "mb_inbox"))).toBe(true);
  });

  it("is not another folder's list, nor any message", () => {
    expect(kept(client, qk.messages("ec_1", "mb_work"))).toBe(false);
    expect(kept(client, qk.message("ec_1", "mb_inbox", "7-1"))).toBe(false);
    expect(kept(client, qk.messages("ec_2", "mb_inbox"))).toBe(false);
  });

  // Another folder's list is read again only when its counts moved: then the server changed
  // what is in it, and the list would disagree with the number beside the folder.
  it("is another folder's list once its counts moved", () => {
    const before = counts(client);
    client.setQueryData(qk.mailboxes("ec_1"), [
      box("mb_inbox", "inbox"),
      { ...box("mb_work", ""), messages: 3, unseen: 2 },
      box("mb_new", ""),
    ]);
    expect(moved(before, counts(client))).toEqual([["ec_1", "mb_work"]]);
  });
});
