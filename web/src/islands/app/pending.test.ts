import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";

import { qk } from "@app/api/keys";
import type { Mailbox, Message, ReadMessage } from "@app/api/types";
import {
  commit,
  countsWithPending,
  listWithPending,
  withPending,
  type Pending,
} from "@app/islands/app/pending";

const message = (id: string, seen = false): Message => ({
  id,
  from: { name: "", email: "a@example.com" },
  to: [],
  subject: id,
  date: 0,
  sent: null,
  seen,
  flagged: false,
  answered: false,
  draft: false,
  has_attachments: false,
  preview: "",
});

const box = (id: string, messages: number, unseen: number): Mailbox => ({
  id,
  name: id,
  path: [id],
  special_use: "",
  selectable: true,
  messages,
  unseen,
  uid_next: 1,
});

const job = (
  p: Partial<Pending> & Pick<Pending, "kind" | "message">,
): Pending => ({
  email_config: "ec_1",
  mailbox: "mb_in",
  value: false,
  target: "",
  seen: false,
  ...p,
});

// What the server said stays as it said it; the pending jobs are drawn over it.
describe("the pending layer", () => {
  const server = [message("1-1"), message("1-2", true), message("1-3")];
  const pending: Pending[] = [
    job({ kind: "seen", message: "1-1", value: true, seen: false }),
    job({ kind: "flagged", message: "1-1", value: true, seen: true }),
    job({ kind: "move", message: "1-2", target: "mb_arc", seen: true }),
    job({ kind: "delete", message: "1-3", seen: false }),
  ];

  it("draws a list with its moves gone and its flags changed, in the order pressed", () => {
    const drawn = listWithPending(server, "mb_in", pending);
    expect(drawn.map((m) => m.id)).toEqual(["1-1"]);
    expect(drawn[0]).toMatchObject({ seen: true, flagged: true });
    expect(server.map((m) => m.id)).toEqual(["1-1", "1-2", "1-3"]);
    expect(server[0]!.seen).toBe(false);
  });

  it("leaves another mailbox's messages alone, though their ids can be the same", () => {
    expect(listWithPending(server, "mb_other", pending)).toEqual(server);
    expect(withPending(server[0]!, "mb_other", pending)).toBe(server[0]);
  });

  it("moves the counts of the mailboxes each action touches", () => {
    const boxes = [box("mb_in", 10, 4), box("mb_arc", 2, 0), box("mb_x", 1, 1)];
    const drawn = countsWithPending(boxes, pending);
    // Read one (-1 unseen), moved a read one out, deleted an unread one.
    expect(drawn[0]).toMatchObject({ messages: 8, unseen: 2 });
    expect(drawn[1]).toMatchObject({ messages: 3, unseen: 0 });
    expect(drawn[2]).toBe(boxes[2]);
  });

  it("draws nothing once nothing is pending", () => {
    const boxes = [box("mb_in", 10, 4)];
    expect(countsWithPending(boxes, [])).toBe(boxes);
    expect(listWithPending(server, "mb_in", [])).toEqual(server);
  });
});

// A message still being read when a job on it is done may answer with what it was before the
// job: it is read again rather than patched, or the pane would keep the old flag for good.
it("reads a message again when a job is done on it while it is being read", async () => {
  const client = new QueryClient();
  const key = qk.message("ec_1", "mb_in", "1-1");
  const reads: ((m: ReadMessage) => void)[] = [];
  const observer = new QueryObserver<ReadMessage>(client, {
    queryKey: key,
    queryFn: () => new Promise<ReadMessage>((resolve) => reads.push(resolve)),
  });
  const stop = observer.subscribe(() => {});
  await Promise.resolve();
  expect(reads).toHaveLength(1);

  commit(client, {
    email_config: "ec_1",
    mailbox: "mb_in",
    message: "1-1",
    kind: "seen",
    value: false,
    target: "",
    seen: true,
  });
  // The first read answers late, with the flag from before the job.
  reads[0]!({
    ...message("1-1", true),
    cc: [],
    mailbox: "mb_in",
    reply_to: [],
    bcc: [],
    text: "",
    html_text: "",
    html: "",
    held_images: 0,
    parts: [],
  });
  await vi.waitFor(() => expect(reads).toHaveLength(2));
  reads[1]!({
    ...message("1-1", false),
    cc: [],
    mailbox: "mb_in",
    reply_to: [],
    bcc: [],
    text: "",
    html_text: "",
    html: "",
    held_images: 0,
    parts: [],
  });
  await vi.waitFor(() =>
    expect(client.getQueryData<ReadMessage>(key)?.seen).toBe(false),
  );
  stop();
});
