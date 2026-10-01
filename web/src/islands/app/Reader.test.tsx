import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@app/api/transport";
import type { Flags, Mailbox, Message, ReadMessage } from "@app/api/types";
import { Reader } from "@app/islands/app/Reader";
import { mount } from "@app/test/harness";

const getMessage = vi.fn();
const patchMessage = vi.fn();

vi.mock("@app/api/actions/emailConfigs", () => ({
  getEmailConfigsByIdMailboxesByMailboxMessagesByMessage: (
    id: string,
    mailbox: string,
    message: string,
  ) => getMessage(id, mailbox, message),
  patchEmailConfigsByIdMailboxesByMailboxMessagesByMessage: (
    id: string,
    mailbox: string,
    message: string,
    body: { seen: boolean },
  ) => patchMessage(id, mailbox, message, body),
  partURL: (id: string, mailbox: string, message: string, section: string) =>
    `/parts/${id}/${mailbox}/${message}/${section}`,
}));

const inbox: Mailbox = {
  id: "mb_inbox",
  name: "INBOX",
  path: ["INBOX"],
  special_use: "inbox",
  selectable: true,
  messages: 1,
  unseen: 0,
};

/** The message as a list has it. */
const listed = (extra: Partial<Message> = {}): Message => ({
  id: "m_1",
  from: { name: "Alice", email: "alice@example.com" },
  to: [{ name: "", email: "misha@example.com" }],
  subject: "Quarterly numbers",
  date: 1790000000,
  sent: null,
  seen: false,
  flagged: false,
  answered: false,
  draft: false,
  has_attachments: true,
  preview: "",
  ...extra,
});

/** What marking it read answers with: its flags on the server now. */
const flags = (seen: boolean): Flags => ({
  id: "m_1",
  seen,
  flagged: false,
  answered: false,
  draft: false,
});

const read = (extra: Partial<ReadMessage> = {}): ReadMessage => ({
  ...listed(),
  cc: [{ name: "Bob", email: "bob@example.com" }],
  mailbox: "mb_inbox",
  text: "The numbers are in.",
  html: "",
  held_images: 0,
  parts: [
    {
      section: "2",
      name: "q3.pdf",
      type: "application/pdf",
      size: 12800,
      listed: true,
    },
    {
      section: "1.2",
      name: "logo.png",
      type: "image/png",
      size: 300,
      listed: false,
    },
  ],
  ...extra,
});

beforeEach(() => {
  getMessage.mockResolvedValue(read());
  patchMessage.mockImplementation(
    async (_c: string, _mb: string, _m: string, body: { seen: boolean }) =>
      flags(body.seen),
  );
});
afterEach(() => vi.clearAllMocks());

const open = () =>
  mount(<Reader config="ec_1" mailbox={inbox} message="m_1" />);

describe("the reading pane", () => {
  it("says who, to whom, and what", async () => {
    open();
    await screen.findByRole("heading", { name: "Quarterly numbers" });
    screen.getByText("Alice");
    screen.getByText("<alice@example.com>");
    screen.getByText("misha@example.com");
    screen.getByText("Bob");
    screen.getByText("The numbers are in.");
  });

  // An image the HTML already shows in place is not an attachment to anybody reading it.
  it("lists the attachments and not the images shown in place", async () => {
    open();
    const list = await screen.findByRole("list", { name: "Attachments" });
    const links = within(list).getAllByRole("link");
    expect(links).toHaveLength(1);
    expect(links[0]!.getAttribute("href")).toBe("/parts/ec_1/mb_inbox/m_1/2");
    within(links[0]!).getByText("q3.pdf");
    within(links[0]!).getByText("13 KB");
  });

  // The frame is the second wall: no scripts, and images only from here.
  it("shows HTML in a frame that runs nothing and loads nothing from elsewhere", async () => {
    getMessage.mockResolvedValue(read({ html: "<p>Rich</p>" }));
    open();
    const frame = await screen.findByTitle<HTMLIFrameElement>("Message");
    expect(frame.getAttribute("sandbox")).not.toContain("allow-scripts");
    const doc = frame.getAttribute("srcdoc")!;
    expect(doc).toContain("<p>Rich</p>");
    expect(doc).toContain(
      `default-src 'none'; img-src 'self' ${window.location.origin} data:;`,
    );
  });

  // Showing them swaps them in where they stand: the message is not fetched again.
  it("asks before loading images from elsewhere", async () => {
    getMessage.mockResolvedValue(
      read({
        html: '<p>Rich <img src="data:image/gif;base64,R0lGOD" data-src="/api/proxy?u=x"></p>',
        held_images: 3,
      }),
    );
    open();
    await screen.findByText("3 images from the internet are not shown.");
    expect(getMessage).toHaveBeenCalledWith("ec_1", "mb_inbox", "m_1");
    fireEvent.click(screen.getByRole("button", { name: "Show images" }));
    await waitFor(() =>
      expect(
        screen.queryByText(/images from the internet are not shown/),
      ).toBeNull(),
    );
    expect(getMessage).toHaveBeenCalledTimes(1);
  });

  it("says what the server said when the message cannot be had", async () => {
    getMessage.mockRejectedValue(
      new ApiError(404, "gone", "This message is no longer on the server."),
    );
    open();
    await screen.findByText("This message is no longer on the server.");
  });

  it("marks an unread message read when it opens", async () => {
    open();
    await waitFor(() =>
      expect(patchMessage).toHaveBeenCalledWith("ec_1", "mb_inbox", "m_1", {
        seen: true,
      }),
    );
    await waitFor(() =>
      expect(
        screen.getByRole<HTMLButtonElement>("button", {
          name: "Mark as unread",
        }).disabled,
      ).toBe(false),
    );
  });

  it("leaves a message that is read as it is", async () => {
    getMessage.mockResolvedValue(read({ seen: true }));
    open();
    await screen.findByRole("heading", { name: "Quarterly numbers" });
    screen.getByRole("button", { name: "Mark as unread" });
    expect(patchMessage).not.toHaveBeenCalled();
  });

  // Marked unread, it stays unread while it is open.
  it("marks it unread on asking, once and for good", async () => {
    open();
    const button = await screen.findByRole<HTMLButtonElement>("button", {
      name: "Mark as unread",
    });
    await waitFor(() => expect(button.disabled).toBe(false));
    fireEvent.click(button);
    await screen.findByRole("button", { name: "Mark as read" });
    await waitFor(() =>
      expect(
        screen.getByRole<HTMLButtonElement>("button", { name: "Mark as read" })
          .disabled,
      ).toBe(false),
    );
    expect(patchMessage.mock.calls.map((c) => c[3])).toEqual([
      { seen: true },
      { seen: false },
    ]);
  });

  it("says what the server said when it would not take it", async () => {
    patchMessage.mockRejectedValue(
      new ApiError(502, "unreachable", "imap.example.com:993 timed out."),
    );
    open();
    await screen.findByText("imap.example.com:993 timed out.");
    screen.getByRole("button", { name: "Mark as read" });
  });
});
