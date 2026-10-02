import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@app/api/transport";
import type {
  JobDraft,
  Mailbox,
  Message,
  Outgoing,
  ReadMessage,
} from "@app/api/types";
import { Compose } from "@app/islands/app/Compose";
import { Notice } from "@app/islands/app/Notice";
import { Reader } from "@app/islands/app/Reader";
import { mount } from "@app/test/harness";

const getMessage = vi.fn();
const getMailboxes = vi.fn();
const postJob = vi.fn();

const postSend = vi.fn();
vi.mock("@app/api/actions/emailConfigs", () => ({
  getEmailConfigsByIdMailboxesByMailboxMessagesByMessage: (
    id: string,
    mailbox: string,
    message: string,
  ) => getMessage(id, mailbox, message),
  getEmailConfigsByIdMailboxes: (id: string) => getMailboxes(id),
  getEmailConfigs: async () => [
    {
      id: "ec_1",
      name: "Work",
      email: "misha@example.com",
      sender_name: "Misha",
      incoming: {
        protocol: "imap",
        host: "imap.example.com",
        port: 993,
        tls: "implicit",
        username: "misha",
      },
      outgoing: {
        host: "smtp.example.com",
        port: 465,
        tls: "implicit",
        username: "",
      },
      created_at: 0,
      updated_at: 0,
      synced_at: null,
      sync_error: "",
    },
  ],
  postEmailConfigsByIdSend: (id: string, body: unknown) => postSend(id, body),
  partURL: (id: string, mailbox: string, message: string, section: string) =>
    `/parts/${id}/${mailbox}/${message}/${section}`,
}));
vi.mock("@app/api/actions/jobs", () => ({
  postJobs: (jobs: JobDraft[]) =>
    Promise.all(jobs.map((body) => postJob(body))),
  getJobs: async () => [],
  deleteJobsById: async () => undefined,
}));

const box = (id: string, special_use: Mailbox["special_use"]): Mailbox => ({
  id,
  name: id,
  path: [id],
  special_use,
  selectable: true,
  messages: 1,
  unseen: 0,
});
const archive = box("mb_archive", "archive");
const trash = box("mb_trash", "trash");
const junk = box("mb_junk", "junk");
const work = box("mb_work", "");

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

const read = (extra: Partial<ReadMessage> = {}): ReadMessage => ({
  ...listed(),
  cc: [{ name: "Bob", email: "bob@example.com" }],
  mailbox: "mb_inbox",
  reply_to: [],
  text: "The numbers are in.",
  html_text: "",
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

let taken = 0;
beforeEach(() => {
  window.history.pushState({}, "", "/c/ec_1/mb_inbox/m_1");
  getMailboxes.mockResolvedValue([inbox, archive, trash, junk, work]);
  getMessage.mockResolvedValue(read());
  // The server takes every job at once, and does it later.
  postJob.mockImplementation(async (body: JobDraft) => ({
    ...body,
    id: `j_${++taken}`,
    error: "",
    created_at: 0,
  }));
});
afterEach(() => vi.clearAllMocks());

const open = (mailbox = inbox) =>
  mount(
    <>
      <Reader config="ec_1" mailbox={mailbox} message="m_1" />
      <Compose />
      <Notice />
    </>,
  );

/** A button, once the message and the folders it can go to have arrived. */
const ready = async (name: string) => {
  const button = await screen.findByRole<HTMLButtonElement>("button", { name });
  await waitFor(() => expect(button.disabled).toBe(false));
  return button;
};

/** Every job asked for, in order. */
const asked = (): JobDraft[] => postJob.mock.calls.map((c) => c[0]);

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

  it("lists the attachments and not the images shown in place", async () => {
    open();
    const list = await screen.findByRole("list", { name: "Attachments" });
    const links = within(list).getAllByRole("link");
    expect(links).toHaveLength(1);
    expect(links[0]!.getAttribute("href")).toBe("/parts/ec_1/mb_inbox/m_1/2");
    within(links[0]!).getByText("q3.pdf");
    within(links[0]!).getByText("13 KB");
  });

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

  it("leaves a message that is read as it is", async () => {
    getMessage.mockResolvedValue(read({ seen: true }));
    open();
    await screen.findByRole("heading", { name: "Quarterly numbers" });
    screen.getByRole("button", { name: "Mark as unread" });
    expect(postJob).not.toHaveBeenCalled();
  });

  it("marks an unread message read when it opens", async () => {
    open();
    await waitFor(() =>
      expect(asked()).toEqual([
        expect.objectContaining({
          email_config: "ec_1",
          mailbox: "mb_inbox",
          message: "m_1",
          kind: "seen",
          value: true,
        }),
      ]),
    );
    await ready("Mark as unread");
  });

  // Marked unread, it stays unread while it is open.
  it("marks it unread on asking, once and for good", async () => {
    open();
    fireEvent.click(await ready("Mark as unread"));
    await ready("Mark as read");
    expect(asked().map((j) => [j.kind, j.value])).toEqual([
      ["seen", true],
      ["seen", false],
    ]);
  });

  // Opened read, it is not read again when it is marked unread while open.
  it("leaves a message opened read unread when asked", async () => {
    getMessage.mockResolvedValue(read({ seen: true }));
    open();
    fireEvent.click(await ready("Mark as unread"));
    await ready("Mark as read");
    expect(asked().map((j) => [j.kind, j.value])).toEqual([["seen", false]]);
  });

  // Archive, Trash and spam are each a move, to the folder the server says is for it; then the
  // message has left this folder, and so has the pane.
  it("archives to the server's archive and goes back to the list", async () => {
    open();
    fireEvent.click(await ready("Archive"));
    await waitFor(() =>
      expect(asked()).toContainEqual(
        expect.objectContaining({ kind: "move", target: "mb_archive" }),
      ),
    );
    await waitFor(() =>
      expect(window.location.pathname).toBe("/c/ec_1/mb_inbox"),
    );
  });

  it("deletes to Trash, and offers spam", async () => {
    open();
    fireEvent.click(await ready("Delete"));
    await waitFor(() =>
      expect(asked()).toContainEqual(
        expect.objectContaining({ kind: "move", target: "mb_trash" }),
      ),
    );
    screen.getByRole("button", { name: "Spam" });
    expect(asked().some((j) => j.kind === "delete")).toBe(false);
  });

  // In Trash there is nowhere further to put it, and that cannot be taken back.
  it("in Trash, deletes for good only after asking", async () => {
    open(trash);
    fireEvent.click(await ready("Delete forever…"));
    const dialog = await screen.findByRole("dialog");
    expect(asked().some((j) => j.kind === "delete")).toBe(false);
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Delete forever" }),
    );
    await waitFor(() =>
      expect(asked()).toContainEqual(
        expect.objectContaining({ kind: "delete", mailbox: "mb_trash" }),
      ),
    );
  });

  it("in Junk, takes it back to the inbox rather than to spam", async () => {
    open(junk);
    fireEvent.click(await ready("Not spam"));
    await waitFor(() =>
      expect(asked()).toContainEqual(
        expect.objectContaining({
          kind: "move",
          mailbox: "mb_junk",
          target: "mb_inbox",
        }),
      ),
    );
    expect(screen.queryByRole("button", { name: "Spam" })).toBeNull();
  });

  // Drawn on the press, and still drawn while the server has it waiting.
  it("stars it, and it stays starred while the server has the job", async () => {
    open();
    const star = await ready("Star");
    expect(star.getAttribute("aria-pressed")).toBe("false");
    fireEvent.click(star);
    await waitFor(() => expect(star.getAttribute("aria-pressed")).toBe("true"));
    await waitFor(() =>
      expect(asked()).toContainEqual(
        expect.objectContaining({ kind: "flagged", value: true }),
      ),
    );
    await waitFor(() => expect(postJob).toHaveBeenCalledTimes(2));
    expect(star.getAttribute("aria-pressed")).toBe("true");
  });

  it("moves it to any other folder", async () => {
    open();
    await ready("Archive");
    const select = screen.getByLabelText<HTMLSelectElement>("Move to folder");
    expect(
      [...select.options].map((o) => o.value).filter(Boolean),
    ).not.toContain("mb_inbox");
    fireEvent.change(select, { target: { value: "mb_work" } });
    await waitFor(() =>
      expect(asked()).toContainEqual(
        expect.objectContaining({ kind: "move", target: "mb_work" }),
      ),
    );
  });

  // Gmail's keys, and not while a field has them.
  it("answers Gmail's keys", async () => {
    open();
    await ready("Archive");
    const select = screen.getByLabelText("Move to folder");
    fireEvent.keyDown(select, { key: "e" });
    expect(asked().some((j) => j.kind === "move")).toBe(false);
    fireEvent.keyDown(document.body, { key: "e" });
    await waitFor(() =>
      expect(asked()).toContainEqual(
        expect.objectContaining({ kind: "move", target: "mb_archive" }),
      ),
    );
  });

  // Not taken by the server: drawn back as it was, and said.
  it("takes a star back and says why when the server will not take it", async () => {
    getMessage.mockResolvedValue(read({ seen: true }));
    postJob.mockRejectedValue(
      new ApiError(503, "unreachable", "emguio cannot be reached right now."),
    );
    open();
    const star = await ready("Star");
    fireEvent.click(star);
    await waitFor(() =>
      expect(star.getAttribute("aria-pressed")).toBe("false"),
    );
    within(await screen.findByRole("alert")).getByText(
      "“Quarterly numbers” was not starred: emguio cannot be reached right now.",
    );
  });

  it("says so when a move it already drew is not taken", async () => {
    getMessage.mockResolvedValue(read({ seen: true }));
    postJob.mockRejectedValue(
      new ApiError(503, "unreachable", "emguio cannot be reached right now."),
    );
    open();
    fireEvent.click(await ready("Archive"));
    within(await screen.findByRole("alert")).getByText(
      "“Quarterly numbers” was not moved: emguio cannot be reached right now.",
    );
  });

  // A reply opens with who and what filled in, and is threaded under the message; what is
  // written above the quote is what goes, and the window closes once the server has it.
  it("replies, threaded under the message", async () => {
    getMessage.mockResolvedValue(read({ seen: true }));
    postSend.mockResolvedValue({ message_id: "<r@example.com>" });
    open();
    fireEvent.click(await screen.findByRole("button", { name: "Reply" }));
    const dialog = within(await screen.findByRole("dialog"));
    dialog.getByRole("heading", { name: "Reply" });
    expect(
      dialog.getByRole<HTMLInputElement>("textbox", { name: "To" }).value,
    ).toBe("Alice <alice@example.com>");
    expect(
      dialog.getByRole<HTMLInputElement>("textbox", { name: "Subject" }).value,
    ).toBe("Re: Quarterly numbers");
    const text = dialog.getByRole<HTMLTextAreaElement>("textbox", {
      name: "Message",
    });
    expect(text.value).toContain("> The numbers are in.");
    fireEvent.change(text, { target: { value: `Thanks!${text.value}` } });
    fireEvent.click(dialog.getByRole("button", { name: "Send" }));

    await waitFor(() => expect(postSend).toHaveBeenCalledTimes(1));
    const [config, body] = postSend.mock.calls[0] as [string, Outgoing];
    expect(config).toBe("ec_1");
    expect(body).toMatchObject({
      to: "Alice <alice@example.com>",
      subject: "Re: Quarterly numbers",
      reply: { mailbox: "mb_inbox", message: "m_1" },
      forward: null,
      attachments: [],
    });
    expect(body.text).toMatch(
      /^Thanks!\n\nOn .*, Alice <alice@example.com> wrote:\n> The numbers are in.\n$/,
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    screen.getByText("Sent.");
  });

  // A forward carries the message's attachments; a refusal is said in the window, and what was
  // written stays to be sent again.
  it("forwards with its attachments, and keeps the window when the server says no", async () => {
    getMessage.mockResolvedValue(read({ seen: true }));
    postSend.mockRejectedValue(
      new ApiError(
        502,
        "unreachable",
        "smtp.example.com:465 refused the message: No such user here",
      ),
    );
    open();
    await screen.findByRole("button", { name: "Forward" });
    fireEvent.keyDown(document.body, { key: "f" });
    const dialog = within(await screen.findByRole("dialog"));
    dialog.getByRole("heading", { name: "Forward" });
    within(dialog.getByRole("list", { name: "Attachments" })).getByText(
      "q3.pdf",
    );
    fireEvent.change(dialog.getByRole("textbox", { name: "To" }), {
      target: { value: "nobody@example.com" },
    });
    fireEvent.click(dialog.getByRole("button", { name: "Send" }));
    await dialog.findByText(/refused the message: No such user here/);
    expect((postSend.mock.calls[0] as [string, Outgoing])[1].forward).toEqual({
      mailbox: "mb_inbox",
      message: "m_1",
      parts: ["2"],
    });
    expect(
      dialog.getByRole<HTMLInputElement>("textbox", { name: "To" }).value,
    ).toBe("nobody@example.com");
  });
});
