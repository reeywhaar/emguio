import { describe, expect, it } from "vitest";

import type { ReadMessage } from "@app/api/types";
import { full } from "@app/format";
import { forwardOf, others, replyTo, typed } from "@app/islands/app/drafts";

const read = (extra: Partial<ReadMessage> = {}): ReadMessage => ({
  id: "7-1",
  mailbox: "mb_inbox",
  from: { name: "Alice", email: "alice@example.com" },
  to: [
    { name: "Misha", email: "Misha@Example.com" },
    { name: "Robin", email: "robin@example.com" },
  ],
  cc: [
    { name: "", email: "kim@example.com" },
    { name: "Alice", email: "alice@example.com" },
  ],
  reply_to: [],
  subject: "Lunch",
  date: 1_790_000_000,
  sent: null,
  seen: true,
  flagged: false,
  answered: false,
  draft: false,
  has_attachments: true,
  preview: "",
  text: "Pizza?\n\nOr sushi.\n",
  html_text: "",
  html: "",
  held_images: 0,
  parts: [
    {
      section: "2",
      name: "menu.pdf",
      type: "application/pdf",
      size: 9,
      listed: true,
    },
    {
      section: "3",
      name: "logo.png",
      type: "image/png",
      size: 9,
      listed: false,
    },
  ],
  ...extra,
});

describe("a reply", () => {
  it("goes to the sender, quoting what they wrote", () => {
    const d = replyTo(read(), "ec_1", "misha@example.com", false);
    expect(d.to).toBe("Alice <alice@example.com>");
    expect(d.cc).toBe("");
    expect(d.subject).toBe("Re: Lunch");
    expect(d.text).toBe(
      `\n\nOn ${full(1_790_000_000)}, Alice <alice@example.com> wrote:\n> Pizza?\n>\n> Or sushi.\n`,
    );
    expect(d.reply).toEqual({ mailbox: "mb_inbox", message: "7-1" });
  });

  // Everybody but self, each once: the sender and the rest of To, then Cc.
  it("to all goes to everybody else it went to", () => {
    const d = replyTo(read(), "ec_1", "misha@example.com", true);
    expect(d.to).toBe("Alice <alice@example.com>, Robin <robin@example.com>");
    expect(d.cc).toBe("kim@example.com");
    expect(others(read(), "misha@example.com")).toBe(true);
    expect(
      others(
        read({ to: [{ name: "", email: "misha@example.com" }], cc: [] }),
        "misha@example.com",
      ),
    ).toBe(false);
  });

  it("goes where the sender asks, and to the recipients of one's own", () => {
    const list = read({
      reply_to: [{ name: "Lunch club", email: "club@example.com" }],
    });
    expect(replyTo(list, "ec_1", "misha@example.com", false).to).toBe(
      "Lunch club <club@example.com>",
    );
    const mine = read({ from: { name: "", email: "misha@example.com" } });
    expect(replyTo(mine, "ec_1", "misha@example.com", false).to).toBe(
      "Robin <robin@example.com>",
    );
  });

  it("is Re: once, and quotes the HTML of a message with no text", () => {
    const d = replyTo(
      read({ subject: "RE: Lunch", text: "", html_text: "From the page." }),
      "ec_1",
      "misha@example.com",
      false,
    );
    expect(d.subject).toBe("RE: Lunch");
    expect(d.text).toContain("> From the page.");
  });
});

it("a forward carries the text and the attachments, not the pictures the HTML shows", () => {
  const d = forwardOf(read(), "ec_1");
  expect(d.subject).toBe("Fwd: Lunch");
  expect(d.to).toBe("");
  expect(d.text).toContain(
    "---------- Forwarded message ----------\nFrom: Alice <alice@example.com>",
  );
  expect(d.text).toContain(
    "Subject: Lunch\nTo: Misha <Misha@Example.com>, Robin <robin@example.com>",
  );
  expect(d.text).toContain("\n\nPizza?\n\nOr sushi.\n");
  expect(d.forward?.parts.map((p) => p.name)).toEqual(["menu.pdf"]);
});

it("an address is quoted where its name would split it", () => {
  expect(typed({ name: "Kim, K.", email: "kim@example.com" })).toBe(
    '"Kim, K." <kim@example.com>',
  );
  expect(typed({ name: "", email: "kim@example.com" })).toBe("kim@example.com");
});
