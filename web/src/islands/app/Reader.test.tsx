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
import { Reader, subjectLook } from "@app/islands/app/Reader";
import { mount } from "@app/test/harness";

const getMessage = vi.fn();
const getMailboxes = vi.fn();
const postJob = vi.fn();

const postSend = vi.fn();
const postDraft = vi.fn();
const getConversation = vi.fn();
const putDraft = vi.fn();
const deleteDraft = vi.fn();
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
      inbox_unseen: 0,
      archive_mailbox: null,
    },
  ],
  postEmailConfigsByIdSend: (id: string, body: unknown) => postSend(id, body),
  getEmailConfigsByIdMailboxesByMailboxMessagesByMessageConversation: (
    id: string,
    mailbox: string,
    message: string,
  ) => getConversation(id, mailbox, message),
  postEmailConfigsByIdDrafts: (id: string, body: unknown) =>
    postDraft(id, body),
  putEmailConfigsByIdDraftsByDraft: (
    id: string,
    draft: string,
    body: unknown,
  ) => putDraft(id, draft, body),
  deleteEmailConfigsByIdDraftsByDraft: (id: string, draft: string) =>
    deleteDraft(id, draft),
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
  uid_next: 1,
});
const archive = box("mb_archive", "archive");
const trash = box("mb_trash", "trash");
const junk = box("mb_junk", "junk");
const work = box("mb_work", "");
const drafts = box("mb_drafts", "drafts");

const inbox: Mailbox = {
  id: "mb_inbox",
  name: "INBOX",
  path: ["INBOX"],
  special_use: "inbox",
  selectable: true,
  messages: 1,
  unseen: 0,
  uid_next: 1,
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
  bcc: [],
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
  getConversation.mockResolvedValue({ messages: [], earlier: 0 });
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
      carry: null,
      draft: null,
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
    expect((postSend.mock.calls[0] as [string, Outgoing])[1].carry).toEqual({
      mailbox: "mb_inbox",
      message: "m_1",
      parts: ["2"],
    });
    expect(
      dialog.getByRole<HTMLInputElement>("textbox", { name: "To" }).value,
    ).toBe("nobody@example.com");
  });
});

/** What each save of a draft asked emguio to keep: the first, then the rest. */
const saved = (): Outgoing[] => [
  ...postDraft.mock.calls.map((c) => (c as [string, Outgoing])[1]),
  ...putDraft.mock.calls.map((c) => (c as [string, string, Outgoing])[2]),
];

const q3 = {
  id: "dp_q3",
  name: "q3.pdf",
  type: "application/pdf",
  size: 12800,
};

describe("drafts", () => {
  // Closing has it put in Drafts; nothing asks.
  it("puts a reply in Drafts on closing", async () => {
    getMessage.mockResolvedValue(read({ seen: true }));
    postDraft.mockResolvedValue({ closed: true });
    open();
    fireEvent.click(await screen.findByRole("button", { name: "Reply" }));
    const dialog = within(await screen.findByRole("dialog"));
    const text = dialog.getByRole<HTMLTextAreaElement>("textbox", {
      name: "Message",
    });
    fireEvent.change(text, { target: { value: `Thanks!${text.value}` } });
    fireEvent.click(dialog.getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    screen.getByText("Saved to Drafts.");
    expect(saved()).toEqual([
      expect.objectContaining({
        reply: { mailbox: "mb_inbox", message: "m_1" },
        draft_id: null,
        close: true,
      }),
    ]);
  });

  // The first save keeps it in emguio with what it carries; the next ones name what it holds,
  // and the message sent is that draft.
  it("keeps it in emguio as it is written, and sends it from there", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      getMessage.mockResolvedValue(read({ seen: true }));
      postDraft.mockResolvedValue({ id: "d_1", parts: [q3], problem: "" });
      putDraft.mockResolvedValue({ id: "d_1", parts: [q3], problem: "" });
      postSend.mockResolvedValue({ message_id: "<f@example.com>" });
      open();
      await screen.findByRole("button", { name: "Forward" });
      fireEvent.keyDown(document.body, { key: "f" });
      const dialog = within(await screen.findByRole("dialog"));
      const to = dialog.getByRole("textbox", { name: "To" });
      fireEvent.change(to, { target: { value: "kim@example.com" } });
      expect(postDraft).not.toHaveBeenCalled();
      await vi.advanceTimersByTimeAsync(2000);
      await dialog.findByText("Saved");
      expect(saved()[0]).toMatchObject({
        to: "kim@example.com",
        draft_id: null,
        carry: { mailbox: "mb_inbox", message: "m_1", parts: ["2"] },
        close: false,
      });

      fireEvent.change(to, {
        target: { value: "kim@example.com, robin@example.com" },
      });
      expect(dialog.queryByText("Saved")).toBeNull();
      await vi.advanceTimersByTimeAsync(2000);
      await waitFor(() => expect(saved()).toHaveLength(2));
      expect(putDraft.mock.calls[0]![1]).toBe("d_1");
      expect(saved()[1]).toMatchObject({
        draft_id: "d_1",
        parts: ["dp_q3"],
        carry: null,
      });
      await dialog.findByText("Saved");

      fireEvent.click(dialog.getByRole("button", { name: "Send" }));
      await waitFor(() => expect(postSend).toHaveBeenCalledTimes(1));
      expect((postSend.mock.calls[0] as [string, Outgoing])[1]).toMatchObject({
        draft_id: "d_1",
        parts: ["dp_q3"],
        carry: null,
      });
    } finally {
      vi.useRealTimers();
    }
  });

  // emguio keeps what the mail server would not take yet, and says so.
  it("closes, saying so, when the mail server takes it later", async () => {
    getMessage.mockResolvedValue(read({ seen: true }));
    postDraft.mockResolvedValue({
      closed: false,
      problem: "imap.example.com cannot be reached.",
    });
    open();
    fireEvent.click(await screen.findByRole("button", { name: "Reply" }));
    const dialog = within(await screen.findByRole("dialog"));
    fireEvent.change(dialog.getByRole("textbox", { name: "Message" }), {
      target: { value: "Thanks!" },
    });
    fireEvent.click(dialog.getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    screen.getByText(
      "Saved. It goes in Drafts once the mail server takes it: imap.example.com cannot be reached.",
    );
  });

  it("opens a draft again as it was, and deletes it when it is discarded", async () => {
    getMailboxes.mockResolvedValue([inbox, drafts]);
    getMessage.mockResolvedValue(
      read({
        seen: true,
        mailbox: "mb_drafts",
        bcc: [{ name: "", email: "secret@example.com" }],
      }),
    );
    open(drafts);
    fireEvent.click(await screen.findByRole("button", { name: "Edit draft" }));
    expect(screen.queryByRole("button", { name: "Reply" })).toBeNull();
    const dialog = within(await screen.findByRole("dialog"));
    dialog.getByRole("heading", { name: "Draft" });
    expect(
      dialog.getByRole<HTMLInputElement>("textbox", { name: "Bcc" }).value,
    ).toBe("secret@example.com");
    within(dialog.getByRole("list", { name: "Attachments" })).getByText(
      "q3.pdf",
    );

    fireEvent.click(dialog.getByRole("button", { name: "Discard" }));
    screen.getByText("Discard this draft?");
    const sure = screen.getAllByRole("button", { name: "Discard" });
    fireEvent.click(sure[sure.length - 1]!);
    await waitFor(() =>
      expect(asked()).toContainEqual(
        expect.objectContaining({
          kind: "delete",
          mailbox: "mb_drafts",
          message: "m_1",
        }),
      ),
    );
    expect(saved()).toEqual([]);
    expect(deleteDraft).not.toHaveBeenCalled();
  });

  // A draft kept in emguio goes from there, and its copy from the mail server.
  it("discards a draft kept in emguio, and its copy in Drafts", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      getMessage.mockResolvedValue(read({ seen: true }));
      postDraft.mockResolvedValue({ id: "d_1", parts: [], problem: "" });
      deleteDraft.mockResolvedValue({
        kept: { mailbox: "mb_drafts", message: "9-100" },
      });
      open();
      fireEvent.click(await screen.findByRole("button", { name: "Reply" }));
      const dialog = within(await screen.findByRole("dialog"));
      fireEvent.change(dialog.getByRole("textbox", { name: "Message" }), {
        target: { value: "Thanks!" },
      });
      await vi.advanceTimersByTimeAsync(2000);
      await dialog.findByText("Saved");
      fireEvent.click(dialog.getByRole("button", { name: "Discard" }));
      const sure = screen.getAllByRole("button", { name: "Discard" });
      fireEvent.click(sure[sure.length - 1]!);
      await waitFor(() =>
        expect(asked()).toContainEqual(
          expect.objectContaining({
            kind: "delete",
            mailbox: "mb_drafts",
            message: "9-100",
          }),
        ),
      );
      expect(deleteDraft).toHaveBeenCalledWith("ec_1", "d_1");
    } finally {
      vi.useRealTimers();
    }
  });

  // Without a Drafts folder, closing asks, as it did before there were drafts.
  it("asks before closing where there is nowhere to keep it", async () => {
    getMessage.mockResolvedValue(read({ seen: true }));
    postDraft.mockRejectedValue(
      new ApiError(
        409,
        "conflict",
        "This mail account has no Drafts folder to keep drafts in.",
      ),
    );
    open();
    fireEvent.click(await screen.findByRole("button", { name: "Reply" }));
    const dialog = within(await screen.findByRole("dialog"));
    fireEvent.change(dialog.getByRole("textbox", { name: "Message" }), {
      target: { value: "Thanks!" },
    });
    fireEvent.click(dialog.getByRole("button", { name: "Close" }));
    await screen.findByText("Discard this message?");
    expect(saved()).toHaveLength(1);
  });
});

describe("a conversation", () => {
  // Oldest first, the open message marked, and one's own replies from Sent among them; another
  // reads in place, and opens from there.
  it("shows the conversation the message is in, and reads another in place", async () => {
    getConversation.mockResolvedValue({
      messages: [
        {
          ...listed({
            id: "m_0",
            from: { name: "Misha", email: "misha@example.com" },
            preview: "Any news?",
            seen: true,
          }),
          mailbox: "mb_sent",
        },
        { ...listed(), mailbox: "mb_inbox" },
      ],
      earlier: 0,
    });
    getMessage.mockImplementation(
      async (_c: string, _mb: string, id: string) =>
        id === "m_0"
          ? read({
              id: "m_0",
              mailbox: "mb_sent",
              text: "Any news on the numbers?",
            })
          : read(),
    );
    open();
    const talk = within(
      await screen.findByRole("region", { name: "Conversation" }),
    );
    const lines = talk.getAllByRole("listitem");
    expect(lines.map((li) => li.getAttribute("aria-current"))).toEqual([
      null,
      "true",
    ]);
    // Opening it reads it, which its line says at once.
    await waitFor(() =>
      expect(talk.getAllByRole("listitem")[1]!.textContent).not.toContain(
        "Unread.",
      ),
    );
    const mine = talk.getByRole("button", { name: /^You/ });
    expect(mine.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(mine);
    await talk.findByText("Any news on the numbers?");
    expect(
      talk
        .getByRole("link", { name: "Open this message" })
        .getAttribute("href"),
    ).toBe("/c/ec_1/mb_sent/m_0");
    expect(getMessage).toHaveBeenCalledWith("ec_1", "mb_sent", "m_0");
  });

  it("shows nothing for a message on its own", async () => {
    open();
    await screen.findByRole("heading", { name: "Quarterly numbers" });
    await waitFor(() => expect(getConversation).toHaveBeenCalled());
    expect(screen.queryByRole("region", { name: "Conversation" })).toBeNull();
  });
});

// A long subject takes fewer lines over the message: smaller, and cut past that.
describe("a subject", () => {
  it("is smaller the longer it is, and cut at three lines past that", () => {
    expect(subjectLook("Lunch?")).toBe("text-xl");
    expect(subjectLook("Quarterly numbers, first look at the draft")).toBe(
      "text-lg",
    );
    expect(
      subjectLook(
        "Новая тарификация билайн, Мотив, Казахстан и Украина с 1 октября 2026",
      ),
    ).toBe("text-base line-clamp-3");
  });
});
