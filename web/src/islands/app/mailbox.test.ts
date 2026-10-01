import { describe, expect, it } from "vitest";

import type { Mailbox } from "@app/api/types";
import { arranged, besideOf, serversOwn } from "@app/islands/app/mailbox";

const box = (...path: string[]): Mailbox => ({
  id: path.join("/"),
  name: path.join("/"),
  path,
  special_use: "",
  selectable: true,
  messages: 0,
  unseen: 0,
  uid_next: 1,
});

const tree = [
  box("INBOX"),
  box("Work"),
  box("Work", "A"),
  box("Work", "A", "Old"),
  box("Work", "B"),
  box("Zebra"),
];
const ids = (boxes: Mailbox[]) => boxes.map((mb) => mb.id);

describe("folders side by side", () => {
  it("are the ones with the same parent", () => {
    expect(ids(besideOf(tree, tree[0]!))).toEqual(["INBOX", "Work", "Zebra"]);
    expect(ids(besideOf(tree, tree[4]!))).toEqual(["Work/A", "Work/B"]);
  });

  it("are put in order, each with what is inside it", () => {
    expect(ids(arranged(tree, ["Zebra", "INBOX", "Work"]))).toEqual([
      "Zebra",
      "INBOX",
      "Work",
      "Work/A",
      "Work/A/Old",
      "Work/B",
    ]);
    expect(ids(arranged(tree, ["Work/B", "Work/A"]))).toEqual([
      "INBOX",
      "Work",
      "Work/B",
      "Work/A",
      "Work/A/Old",
      "Zebra",
    ]);
  });
});

describe("the server's own folders", () => {
  it("are those with a use, and those holding one", () => {
    const gmail = [
      { ...box("INBOX"), special_use: "inbox" as const },
      box("[Gmail]"),
      { ...box("[Gmail]", "Sent Mail"), special_use: "sent" as const },
      box("[Gmail]", "Important"),
      box("Work"),
    ];
    expect(
      gmail.filter((mb) => serversOwn(gmail, mb)).map((mb) => mb.id),
    ).toEqual(["INBOX", "[Gmail]", "[Gmail]/Sent Mail"]);
  });
});
