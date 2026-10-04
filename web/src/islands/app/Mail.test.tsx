import {
  act,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { qk } from "@app/api/keys";
import { ApiError } from "@app/api/transport";
import type {
  EmailConfig,
  Job,
  JobDraft,
  Mailbox,
  Message,
} from "@app/api/types";
import { Jobs } from "@app/islands/app/Jobs";
import { Mail } from "@app/islands/app/Mail";
import { Notice } from "@app/islands/app/Notice";
import { useRoute } from "@app/islands/app/route";
import { mount } from "@app/test/harness";
import { reach } from "@app/test/view";

const getEmailConfigs = vi.fn();
const getEmailConfigsByIdMailboxes = vi.fn();
const getMessages = vi.fn();
const postEmailConfigsByIdSync = vi.fn();
const getMessage = vi.fn();
const getThreads = vi.fn();
const postMailbox = vi.fn();
const putMailbox = vi.fn();
const deleteMailbox = vi.fn();
const putOrder = vi.fn();
const postJob = vi.fn();
const postJobsRequest = vi.fn();
const getJobs = vi.fn();
const dismissJob = vi.fn();

vi.mock("@app/api/actions/emailConfigs", () => ({
  getEmailConfigs: () => getEmailConfigs(),
  getEmailConfigsByIdMailboxes: (id: string) =>
    getEmailConfigsByIdMailboxes(id),
  getEmailConfigsByIdMailboxesByMailboxMessages: (
    id: string,
    mailbox: string,
    cursor: string,
    q: string,
  ) => getMessages(id, mailbox, cursor, q),
  postEmailConfigsByIdSync: (id: string) => postEmailConfigsByIdSync(id),
  postEmailConfigsByIdMailboxes: (id: string, body: unknown) =>
    postMailbox(id, body),
  putEmailConfigsByIdMailboxesByMailbox: (
    id: string,
    mailbox: string,
    body: unknown,
  ) => putMailbox(id, mailbox, body),
  deleteEmailConfigsByIdMailboxesByMailbox: (id: string, mailbox: string) =>
    deleteMailbox(id, mailbox),
  putEmailConfigsByIdMailboxesOrder: (id: string, ids: string[]) =>
    putOrder(id, ids),
  getEmailConfigsByIdMailboxesByMailboxThreads: (
    id: string,
    mailbox: string,
    messages: string[],
  ) => getThreads(id, mailbox, messages),
  getEmailConfigsByIdMailboxesByMailboxMessagesByMessageConversation:
    async () => ({ messages: [], earlier: 0 }),
  getEmailConfigsByIdMailboxesByMailboxMessagesByMessage: (
    id: string,
    mailbox: string,
    message: string,
  ) => getMessage(id, mailbox, message),
  partURL: (id: string, mailbox: string, message: string, section: string) =>
    `/parts/${id}/${mailbox}/${message}/${section}`,
}));
vi.mock("@app/api/actions/jobs", () => ({
  postJobs: (jobs: JobDraft[]) => {
    postJobsRequest(jobs);
    return Promise.all(jobs.map((body) => postJob(body)));
  },
  getJobs: () => getJobs(),
  deleteJobsById: (id: string) => dismissJob(id),
}));

/** What the server answers a job with: taken, and waiting. */
let taken = 0;
const take = async (body: JobDraft): Promise<Job> => ({
  ...body,
  id: `j_${++taken}`,
  error: "",
  created_at: 0,
});

const work: EmailConfig = {
  id: "ec_1",
  name: "Work",
  email: "misha@example.com",
  sender_name: "",
  incoming: {
    protocol: "imap",
    host: "imap.example.com",
    port: 993,
    tls: "implicit",
    username: "misha",
  },
  outgoing: null,
  created_at: 0,
  updated_at: 0,
  synced_at: Math.round(Date.now() / 1000) - 120,
  sync_error: "",
  inbox_unseen: 0,
  archive_mailbox: null,
};

const box = (
  id: string,
  path: string[],
  special_use: Mailbox["special_use"] = "",
  unseen = 0,
): Mailbox => ({
  id,
  name: path.join("/"),
  path,
  special_use,
  selectable: true,
  messages: 10,
  unseen,
  uid_next: 1,
});

const boxes = [
  box("mb_inbox", ["INBOX"], "inbox", 2),
  box("mb_sent", ["Sent"], "sent"),
  box("mb_work", ["Work"]),
  box("mb_clients", ["Work", "Clients"], "", 1),
];

const message = (
  id: string,
  subject: string,
  extra: Partial<Message> = {},
): Message => ({
  id,
  from: { name: "Alice", email: "alice@example.com" },
  to: [{ name: "Bob", email: "bob@example.com" }],
  subject,
  date: Math.round(Date.now() / 1000),
  sent: null,
  seen: true,
  flagged: false,
  answered: false,
  draft: false,
  has_attachments: false,
  preview: "",
  ...extra,
});

beforeEach(() => {
  window.history.pushState({}, "", "/c/ec_1");
  getEmailConfigs.mockResolvedValue([work]);
  getEmailConfigsByIdMailboxes.mockResolvedValue(boxes);
  getMessages.mockResolvedValue({
    messages: [
      message("m_1", "Quarterly numbers", {
        seen: false,
        has_attachments: true,
      }),
      message("m_2", "Lunch?", { flagged: true }),
    ],
  });
  postEmailConfigsByIdSync.mockResolvedValue(undefined);
  getThreads.mockResolvedValue({ counts: {} });
});

afterEach(() => vi.clearAllMocks());

// A folder of three, and one of them open beside it.
const three = () =>
  getMessages.mockResolvedValue({
    messages: [
      message("m_1", "First"),
      message("m_2", "Second"),
      message("m_3", "Third"),
    ],
  });
const opened = (id: string) => {
  getMessage.mockResolvedValue({
    ...message(id, "Open"),
    cc: [],
    mailbox: "mb_inbox",
    text: "Hello.",
    html: "",
    reply_to: [],
    html_text: "",
    held_images: 0,
    parts: [],
  });
  // The server takes the job and never says it is done: what shows is what the press drew.
  postJob.mockImplementation(take);
  return mount(<Mail named="ec_1" mailbox="mb_inbox" message={id} />);
};
const moveAway = async () => {
  const reader = await screen.findByRole("article", { name: "Message" });
  await within(reader).findByText("Hello.");
  const picker =
    await within(reader).findByLabelText<HTMLSelectElement>("Move to folder");
  await waitFor(() => expect(picker.disabled).toBe(false));
  fireEvent.change(picker, { target: { value: "mb_work" } });
};
const subjectsListed = () =>
  within(screen.getByRole("list", { name: "Messages" }))
    .getAllByRole("listitem")
    .map((li) => li.textContent);

// The mail view as the address says, as the application draws it.
function Routed() {
  const r = useRoute();
  return r.page === "mail" ? (
    <Mail named={r.config} mailbox={r.mailbox} message={r.message} q={r.q} />
  ) : null;
}

// Moves the open message to Work, once it has arrived.
const moveOpen = async (id: string) => {
  const reader = await screen.findByRole("article", { name: "Message" });
  await within(reader).findByText(`Text of ${id}`);
  const picker =
    await within(reader).findByLabelText<HTMLSelectElement>("Move to folder");
  await waitFor(() => expect(picker.disabled).toBe(false));
  fireEvent.change(picker, { target: { value: "mb_work" } });
};

// Opens the inbox and starts selecting; the list it gives is the rows.
const start = async () => {
  mount(<Mail named="ec_1" mailbox="mb_inbox" message={null} />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Select messages" }),
  );
  return within(await screen.findByRole("list", { name: "Messages" }));
};
/** What the selection's header says of how many are selected. */
const selectedCount = () =>
  screen.getByRole("checkbox", { name: "Select all" }).closest("label")!
    .textContent;
const checkbox = (list: ReturnType<typeof within>, name: string) =>
  list.getByRole("checkbox", { name }) as HTMLInputElement;

// The sidebar's folders, with what is said of them.
const sidebar = async () => {
  mount(
    <>
      <Mail named="ec_1" mailbox="mb_inbox" message={null} />
      <Notice />
    </>,
  );
  const nav = await screen.findByRole("navigation", { name: "Folders" });
  await within(nav).findByRole("link", { name: /^Work/ });
  return within(nav);
};
// A pointer pressed on a folder and moved; where it is over is elementFromPoint's to say.
const press = (el: Element, pointerType: string, clientY = 50) =>
  fireEvent.pointerDown(el, {
    button: 0,
    pointerId: 1,
    pointerType,
    clientX: 10,
    clientY,
  });
const move = (el: Element, pointerType: string, clientY: number) =>
  fireEvent.pointerMove(el, {
    pointerId: 1,
    pointerType,
    clientX: 10,
    clientY,
  });
// Long enough for a request that was going to be made to have been.
const settled = () => act(() => new Promise((done) => setTimeout(done, 20)));
const names = (nav: ReturnType<typeof within>) =>
  nav
    .getAllByRole("link")
    .map((l: HTMLElement) => l.textContent?.split(",")[0]);

describe("the mail view", () => {
  it("opens on the inbox and lists its messages", async () => {
    mount(<Mail named="ec_1" mailbox={null} message={null} />);
    const list = await screen.findByRole("list", { name: "Messages" });
    expect(getMessages).toHaveBeenCalledWith("ec_1", "mb_inbox", "", "");
    const rows = within(list).getAllByRole("listitem");
    expect(rows).toHaveLength(2);
    within(rows[0]!).getByText("Unread.");
    within(rows[0]!).getByText("Quarterly numbers");
    within(rows[0]!).getByRole("img", { name: "Attachment" });
    within(rows[1]!).getByRole("img", { name: "Flagged" });
    expect(within(rows[1]!).queryByText("Unread.")).toBeNull();
  });

  // Asked for once the rows show, a page at a time, and drawn beside the date.
  it("tells how many messages each is in a conversation with", async () => {
    getThreads.mockResolvedValue({ counts: { m_1: 3 } });
    mount(<Mail named="ec_1" mailbox={null} message={null} />);
    const rows = within(
      await screen.findByRole("list", { name: "Messages" }),
    ).getAllByRole("listitem");
    await waitFor(() =>
      expect(rows[0]!.textContent).toContain("Conversation of 3."),
    );
    expect(getThreads).toHaveBeenCalledWith("ec_1", "mb_inbox", ["m_1", "m_2"]);
    expect(rows[1]!.textContent).not.toContain("Conversation");
  });

  // A server that names no archive archives to the folder chosen for it.
  it("archives to the folder chosen for it", async () => {
    getEmailConfigs.mockResolvedValue([
      { ...work, archive_mailbox: "mb_clients" },
    ]);
    opened("m_1");
    mount(<Mail named="ec_1" mailbox="mb_inbox" message="m_1" />);
    const reader = await screen.findByRole("article", { name: "Message" });
    const archive = await within(reader).findByRole("button", {
      name: "Archive",
    });
    await waitFor(() => expect(archive).toHaveProperty("disabled", false));
    fireEvent.click(archive);
    await waitFor(() =>
      expect(postJob).toHaveBeenCalledWith(
        expect.objectContaining({ kind: "move", target: "mb_clients" }),
      ),
    );
  });

  // A folder of one's own is renamed or moved, never inside itself.
  it("renames and moves a folder of one's own", async () => {
    putMailbox.mockResolvedValue(box("mb_work", ["Projects"]));
    window.history.pushState({}, "", "/c/ec_1/mb_work");
    mount(<Routed />);
    const menu = await screen.findByRole<HTMLSelectElement>("combobox", {
      name: "Folder",
    });
    fireEvent.change(menu, { target: { value: "edit" } });
    const dialog = within(await screen.findByRole("dialog"));
    dialog.getByRole("heading", { name: "Rename or move folder" });
    const name = dialog.getByLabelText<HTMLInputElement>("Name");
    expect(name.value).toBe("Work");
    expect(
      [...dialog.getByLabelText<HTMLSelectElement>("Inside").options].map((o) =>
        o.textContent?.trim(),
      ),
    ).toEqual(["No folder, at the top", "Inbox", "Sent"]);
    fireEvent.change(name, { target: { value: "Projects" } });
    fireEvent.click(dialog.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(putMailbox).toHaveBeenCalledWith("ec_1", "mb_work", {
        name: "Projects",
        parent: "",
      }),
    );
  });

  it("deletes a folder of one's own, after asking", async () => {
    deleteMailbox.mockResolvedValue(undefined);
    window.history.pushState({}, "", "/c/ec_1/mb_work");
    mount(<Routed />);
    fireEvent.change(
      await screen.findByRole<HTMLSelectElement>("combobox", {
        name: "Folder",
      }),
      { target: { value: "delete" } },
    );
    await screen.findByText("Delete Work?");
    const sure = screen.getAllByRole("button", { name: "Delete" });
    fireEvent.click(sure[sure.length - 1]!);
    await waitFor(() =>
      expect(deleteMailbox).toHaveBeenCalledWith("ec_1", "mb_work"),
    );
    await waitFor(() => expect(window.location.pathname).toBe("/c/ec_1"));
  });

  it("offers nothing of the kind for the server's own folders", async () => {
    window.history.pushState({}, "", "/c/ec_1/mb_sent");
    mount(<Routed />);
    await screen.findAllByText("To: Bob");
    expect(screen.queryByRole("combobox", { name: "Folder" })).toBeNull();
  });

  // From the sidebar a folder is made on its own, nothing moved, and opened.
  it("makes a folder and opens it", async () => {
    postMailbox.mockResolvedValue(box("mb_projects", ["Projects"]));
    mount(<Routed />);
    fireEvent.click(await screen.findByRole("button", { name: "New folder" }));
    const dialog = within(await screen.findByRole("dialog"));
    dialog.getByRole("heading", { name: "New folder" });
    fireEvent.change(dialog.getByLabelText("Inside"), {
      target: { value: "mb_work" },
    });
    fireEvent.change(dialog.getByLabelText("Name"), {
      target: { value: "Projects" },
    });
    fireEvent.click(dialog.getByRole("button", { name: "Make" }));
    await waitFor(() =>
      expect(window.location.pathname).toBe("/c/ec_1/mb_projects"),
    );
    expect(postMailbox).toHaveBeenCalledWith("ec_1", {
      name: "Projects",
      parent: "mb_work",
    });
    expect(postJob).not.toHaveBeenCalled();
  });

  it("moves to a folder made for it", async () => {
    postMailbox.mockResolvedValue(box("mb_projects", ["Projects"]));
    opened("m_1");
    mount(<Mail named="ec_1" mailbox="mb_inbox" message="m_1" />);
    const reader = await screen.findByRole("article", { name: "Message" });
    await within(reader).findByText("Hello.");
    const picker =
      await within(reader).findByLabelText<HTMLSelectElement>("Move to folder");
    await waitFor(() => expect(picker.disabled).toBe(false));
    fireEvent.change(picker, { target: { value: "new" } });
    const dialog = within(await screen.findByRole("dialog"));
    dialog.getByRole("heading", { name: "Move to a new folder" });
    fireEvent.change(dialog.getByLabelText("Name"), {
      target: { value: " Projects " },
    });
    fireEvent.click(dialog.getByRole("button", { name: "Make and move" }));
    await waitFor(() =>
      expect(postJob).toHaveBeenCalledWith(
        expect.objectContaining({ kind: "move", target: "mb_projects" }),
      ),
    );
    expect(postMailbox).toHaveBeenCalledWith("ec_1", {
      name: "Projects",
      parent: "",
    });
  });

  it("lists folders in the server's tree, with what is unread and how many", async () => {
    mount(<Mail named="ec_1" mailbox={null} message={null} />);
    const nav = await screen.findByRole("navigation", { name: "Folders" });
    const links = await within(nav).findAllByRole("link");
    expect(links.map((l) => l.textContent)).toEqual([
      "Inbox, unread: 2, messages: /10",
      "Sent, messages: 10",
      "Work, messages: 10",
      "Clients, unread: 1, messages: /10",
    ]);
    expect(links[0]!.getAttribute("aria-current")).toBe("page");
  });

  // On a phone the folder open is a button, and the folders a list in a dialog.
  it("on a phone, chooses another folder from a list", async () => {
    window.history.pushState({}, "", "/c/ec_1/mb_inbox");
    mount(<Routed />);
    const open = await screen.findByRole("button", {
      name: /^Folder:\s*Inbox/,
    });
    expect(open.textContent).toBe("Folder: Inbox, unread: 2, messages: /10");
    fireEvent.click(open);
    const dialog = within(await screen.findByRole("dialog"));
    dialog.getByRole("heading", { name: "Folders" });
    expect(dialog.getAllByRole("link").map((l) => l.textContent)).toEqual([
      "Inbox, unread: 2, messages: /10",
      "Sent, messages: 10",
      "Work, messages: 10",
      "Clients, unread: 1, messages: /10",
    ]);
    fireEvent.click(dialog.getByRole("link", { name: /^Clients/ }));
    expect(window.location.pathname).toBe("/c/ec_1/mb_clients");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("names the folder and the account in the tab, and gives the tab back", async () => {
    document.title = "emguio";
    const { unmount } = mount(<Routed />);
    await waitFor(() => expect(document.title).toBe("Inbox | Work"));
    const nav = await screen.findByRole("navigation", { name: "Folders" });
    fireEvent.click(within(nav).getByRole("link", { name: /^Sent/ }));
    await waitFor(() => expect(document.title).toBe("Sent | Work"));
    unmount();
    expect(document.title).toBe("emguio");
  });

  it("opens another folder from the sidebar, and the address says which", async () => {
    mount(<Mail named="ec_1" mailbox={null} message={null} />);
    const nav = await screen.findByRole("navigation", { name: "Folders" });
    fireEvent.click(await within(nav).findByRole("link", { name: /Clients/ }));
    expect(window.location.pathname).toBe("/c/ec_1/mb_clients");
  });

  describe("dragging a folder", () => {
    // Every row is somewhere to land; which one is the test's to say.
    let under: Element | null = null;
    const listed = [...boxes, box("mb_home", ["Home"])];
    beforeEach(() => {
      document.elementFromPoint = () => under;
      getEmailConfigsByIdMailboxes.mockResolvedValue(listed);
      putOrder.mockImplementation(async (_id: string, ids: string[]) => [
        ...listed.filter((mb) => mb.special_use),
        ...ids.flatMap((id) => {
          const top = listed.find((mb) => mb.id === id)!;
          return listed.filter((mb) => mb.path[0] === top.path[0]);
        }),
      ]);
    });
    const drag = (
      nav: ReturnType<typeof within>,
      from: RegExp,
      to: RegExp,
      pointerType = "mouse",
    ) => {
      const folder = nav.getByRole("link", { name: from });
      under = nav.getByRole("link", { name: to });
      press(folder, pointerType);
      move(folder, pointerType, 10);
      fireEvent.pointerUp(folder, { pointerId: 1 });
      return folder;
    };

    it("puts one of the user's among theirs, with what is inside it, and does not open it", async () => {
      const nav = await sidebar();
      fireEvent.click(drag(nav, /^Home/, /^Work/));
      await waitFor(() =>
        expect(putOrder).toHaveBeenCalledWith("ec_1", ["mb_home", "mb_work"]),
      );
      expect(names(nav)).toEqual(["Inbox", "Sent", "Home", "Work", "Clients"]);
      expect(window.location.pathname).toBe("/c/ec_1");
    });

    it("leaves the server's own where they are, and lands only beside its own", async () => {
      const nav = await sidebar();
      drag(nav, /^Sent/, /^Inbox/);
      drag(nav, /^Work/, /^Inbox/);
      drag(nav, /^Clients/, /^Home/);
      await settled();
      expect(putOrder).not.toHaveBeenCalled();
      expect(names(nav)).toEqual(["Inbox", "Sent", "Work", "Clients", "Home"]);
    });

    it("by finger, once it has rested; one that moves at once is scrolling", async () => {
      const nav = await sidebar();
      drag(nav, /^Home/, /^Work/, "touch");
      await settled();
      expect(putOrder).not.toHaveBeenCalled();

      const folder = nav.getByRole("link", { name: /^Home/ });
      press(folder, "touch");
      await act(() => new Promise((done) => setTimeout(done, 450)));
      move(folder, "touch", 10);
      fireEvent.pointerUp(folder, { pointerId: 1 });
      await waitFor(() =>
        expect(putOrder).toHaveBeenCalledWith("ec_1", ["mb_home", "mb_work"]),
      );
    });

    it("goes back where it was when the server refuses", async () => {
      putOrder.mockRejectedValue(
        new ApiError(
          400,
          "invalid",
          "Only folders side by side can be put in order.",
        ),
      );
      const nav = await sidebar();
      drag(nav, /^Home/, /^Work/);
      await screen.findByText("Only folders side by side can be put in order.");
      expect(names(nav)).toEqual(["Inbox", "Sent", "Work", "Clients", "Home"]);
    });
  });

  // In mail somebody sent, the useful name is who it went to.
  it("shows who sent mail went to", async () => {
    mount(<Mail named="ec_1" mailbox="mb_sent" message={null} />);
    await screen.findAllByText("To: Bob");
  });

  // A search is the server's, in the folder open; the address keeps it, so a result opened has
  // the results beside it, and Escape goes back to the whole folder.
  it("searches the folder open", async () => {
    getMessages.mockImplementation(
      async (_c: string, _m: string, _cursor: string, q: string) => ({
        messages: q
          ? [message("m_2", "Lunch?")]
          : [message("m_1", "Quarterly numbers"), message("m_2", "Lunch?")],
      }),
    );
    window.history.pushState({}, "", "/c/ec_1/mb_inbox");
    mount(<Routed />);
    await screen.findByText("Quarterly numbers");

    fireEvent.keyDown(document.body, { key: "/" });
    const field = screen.getByRole("searchbox", { name: "Search Inbox" });
    expect(document.activeElement).toBe(field);
    fireEvent.change(field, { target: { value: " from:alice lunch " } });
    fireEvent.submit(field.closest("form")!);
    expect(window.location.search).toBe("?q=from%3Aalice+lunch");
    const found = within(await screen.findByRole("list", { name: "Messages" }));
    expect(found.getAllByRole("link")).toHaveLength(1);
    expect(getMessages).toHaveBeenLastCalledWith(
      "ec_1",
      "mb_inbox",
      "",
      "from:alice lunch",
    );

    fireEvent.click(found.getByRole("link"));
    expect(window.location.pathname + window.location.search).toBe(
      "/c/ec_1/mb_inbox/m_2?q=from%3Aalice+lunch",
    );
    expect(
      screen.getByRole<HTMLInputElement>("searchbox", { name: "Search Inbox" })
        .value,
    ).toBe("from:alice lunch");

    fireEvent.keyDown(screen.getByRole("searchbox", { name: "Search Inbox" }), {
      key: "Escape",
    });
    await screen.findByText("Quarterly numbers");
    expect(window.location.pathname + window.location.search).toBe(
      "/c/ec_1/mb_inbox",
    );
  });

  // Unread only is is:unread in the query, kept apart from what is typed and kept while searching.
  it("shows unread mail only, while searching too", async () => {
    getMessages.mockImplementation(
      async (_c: string, _m: string, _cursor: string, q: string) => ({
        messages: q.includes("lunch")
          ? []
          : q
            ? [message("m_1", "Quarterly numbers", { seen: false })]
            : [
                message("m_1", "Quarterly numbers", { seen: false }),
                message("m_2", "Lunch?"),
              ],
      }),
    );
    window.history.pushState({}, "", "/c/ec_1/mb_inbox");
    mount(<Routed />);
    await screen.findByText("Lunch?");
    const toggle = screen.getByRole("button", { name: "Unread only" });
    expect(toggle.getAttribute("aria-pressed")).toBe("false");

    fireEvent.click(toggle);
    expect(window.location.search).toBe("?q=is%3Aunread");
    await waitFor(() => expect(screen.queryByText("Lunch?")).toBeNull());
    expect(getMessages).toHaveBeenLastCalledWith(
      "ec_1",
      "mb_inbox",
      "",
      "is:unread",
    );
    expect(
      screen
        .getByRole("button", { name: "Unread only" })
        .getAttribute("aria-pressed"),
    ).toBe("true");
    const field = screen.getByRole<HTMLInputElement>("searchbox", {
      name: "Search Inbox",
    });
    expect(field.value).toBe("");

    fireEvent.change(field, { target: { value: "lunch" } });
    fireEvent.submit(field.closest("form")!);
    expect(window.location.search).toBe("?q=lunch+is%3Aunread");
    await screen.findByText("Nothing here matches.");
    expect(
      screen.getByRole<HTMLInputElement>("searchbox", { name: "Search Inbox" })
        .value,
    ).toBe("lunch");

    fireEvent.click(screen.getByRole("button", { name: "Unread only" }));
    expect(window.location.search).toBe("?q=lunch");
  });

  it("says when nothing is unread", async () => {
    getMessages.mockResolvedValue({ messages: [] });
    window.history.pushState({}, "", "/c/ec_1/mb_inbox?q=is%3Aunread");
    mount(<Routed />);
    await screen.findByText("No unread messages here.");
  });

  // The end of the list is rows still to come, drawn before they are here, and reaching it
  // brings them; past the last there is nothing more to draw.
  it("reaches further back as the end of the list comes into view", async () => {
    getMessages.mockImplementation(
      async (_c: string, _m: string, cursor: string) =>
        cursor === ""
          ? { messages: [message("m_1", "Newer")], next_cursor: "c1" }
          : { messages: [message("m_2", "Older")] },
    );
    mount(<Mail named="ec_1" mailbox={null} message={null} />);
    await screen.findByText("Newer");
    screen.getByText("Loading older messages.");
    expect(getMessages).toHaveBeenCalledTimes(1);

    act(reach);
    await screen.findByText("Older");
    expect(getMessages).toHaveBeenLastCalledWith("ec_1", "mb_inbox", "c1", "");
    expect(screen.queryByText("Loading older messages.")).toBeNull();
  });

  // Underway until the server says the look is done, though it found nothing new.
  it("asks for a look now, and says when it is done", async () => {
    const { client } = mount(
      <Mail named="ec_1" mailbox={null} message={null} />,
    );
    const button = (
      await screen.findAllByRole("button", { name: "Fetch new mail" })
    )[0]!;
    fireEvent.click(button);
    await waitFor(() =>
      expect(postEmailConfigsByIdSync).toHaveBeenCalledWith("ec_1"),
    );
    expect(screen.getAllByText("Updating…").length).toBeGreaterThan(0);
    expect(button.hasAttribute("disabled")).toBe(true);

    getEmailConfigs.mockResolvedValue([
      { ...work, synced_at: Math.round(Date.now() / 1000) },
    ]);
    await act(() => client.invalidateQueries({ queryKey: qk.emailConfigs }));
    await waitFor(() =>
      expect(screen.getAllByText("Updated just now").length).toBeGreaterThan(0),
    );
    expect(button.hasAttribute("disabled")).toBe(false);
  });

  // New mail is lit for a moment as it comes in; what was there already is not.
  it("lights mail that has just come in", async () => {
    const { client } = mount(
      <Mail named="ec_1" mailbox="mb_inbox" message={null} />,
    );
    const list = within(await screen.findByRole("list", { name: "Messages" }));
    expect(
      list.getAllByRole("listitem")[0]!.querySelector(".arrived"),
    ).toBeNull();

    getMessages.mockResolvedValue({
      messages: [
        message("m_3", "Just in", { seen: false }),
        message("m_1", "Quarterly numbers"),
        message("m_2", "Lunch?"),
      ],
    });
    await act(() =>
      client.invalidateQueries({
        queryKey: qk.messages("ec_1", "mb_inbox", ""),
      }),
    );
    await screen.findByText("Just in");
    const rows = within(
      screen.getByRole("list", { name: "Messages" }),
    ).getAllByRole("listitem");
    expect(rows[0]!.querySelector(".arrived")).not.toBeNull();
    expect(rows[1]!.querySelector(".arrived")).toBeNull();
  });

  it("says why the mail could not be fetched, and where to fix it", async () => {
    getEmailConfigs.mockResolvedValue([
      {
        ...work,
        sync_error:
          "imap.example.com:993 refused the username or password: no.",
      },
    ]);
    mount(<Mail named="ec_1" mailbox={null} message={null} />);
    const alert = await screen.findByRole("alert");
    within(alert).getByText(/refused the username or password/);
    expect(
      within(alert)
        .getByRole("link", { name: "Check its settings" })
        .getAttribute("href"),
    ).toBe("/settings/email-configs/ec_1");
  });

  it("says it is still fetching before the first look has finished", async () => {
    getEmailConfigs.mockResolvedValue([{ ...work, synced_at: null }]);
    getEmailConfigsByIdMailboxes.mockResolvedValue([]);
    mount(<Mail named="ec_1" mailbox={null} message={null} />);
    await screen.findByText("Fetching folders from imap.example.com…");
  });

  it("says so when a folder in the address is not there", async () => {
    mount(<Mail named="ec_1" mailbox="mb_gone" message={null} />);
    await screen.findByText("There is no such folder.");
  });

  it("opens a message from its row, and the address says which", async () => {
    mount(<Mail named="ec_1" mailbox={null} message={null} />);
    const list = await screen.findByRole("list", { name: "Messages" });
    const link = within(list).getAllByRole("link")[1]!;
    expect(link.getAttribute("href")).toBe("/c/ec_1/mb_inbox/m_2");
    fireEvent.click(link);
    expect(window.location.pathname).toBe("/c/ec_1/mb_inbox/m_2");
  });

  it("shows the open message beside its list", async () => {
    getMessage.mockResolvedValue({
      ...message("m_2", "Lunch?"),
      cc: [],
      mailbox: "mb_inbox",
      text: "Thursday at one?",
      html: "",
      reply_to: [],
      html_text: "",
      held_images: 0,
      parts: [],
    });
    mount(<Mail named="ec_1" mailbox="mb_inbox" message="m_2" />);
    const reader = await screen.findByRole("article", { name: "Message" });
    await within(reader).findByText("Thursday at one?");
    const list = screen.getByRole("list", { name: "Messages" });
    expect(
      within(list).getAllByRole("link")[1]!.getAttribute("aria-current"),
    ).toBe("true");
  });

  // A moved message leaves the list on the press, and the pane goes on to the one below it, or
  // else the one above, or else the folder — before the server has answered.
  describe("after a message is moved away", () => {
    it("goes on to the one below it", async () => {
      three();
      opened("m_2");
      await moveAway();
      await waitFor(() =>
        expect(window.location.pathname).toBe("/c/ec_1/mb_inbox/m_3"),
      );
      expect(subjectsListed().some((t) => t?.includes("Second"))).toBe(false);
    });

    it("or to the one above, when it was the last", async () => {
      three();
      opened("m_3");
      await moveAway();
      await waitFor(() =>
        expect(window.location.pathname).toBe("/c/ec_1/mb_inbox/m_2"),
      );
    });

    it("or back to the folder, when it was the only one", async () => {
      getMessages.mockResolvedValue({ messages: [message("m_1", "Only")] });
      opened("m_1");
      await moveAway();
      await waitFor(() =>
        expect(window.location.pathname).toBe("/c/ec_1/mb_inbox"),
      );
    });

    // On a phone the message is open in the list's place, and the list is what comes back.
    it("or back to the folder on a phone, whatever is below it", async () => {
      const wide = window.innerWidth;
      window.innerWidth = 390;
      try {
        three();
        opened("m_2");
        await moveAway();
        await waitFor(() =>
          expect(window.location.pathname).toBe("/c/ec_1/mb_inbox"),
        );
      } finally {
        window.innerWidth = wide;
      }
    });
  });

  // A pile of jobs is drawn as done on the press and taken by the server in the order pressed.
  // When the server has done the first and failed the last, the first stays done and only the
  // last comes back, with the reason.
  it("draws a pile of moves, and a failed last one undoes only itself", async () => {
    three();
    getMessage.mockImplementation(
      async (_c: string, _mb: string, id: string) => ({
        ...message(id, `Open ${id}`),
        cc: [],
        mailbox: "mb_inbox",
        text: `Text of ${id}`,
        html: "",
        reply_to: [],
        html_text: "",
        held_images: 0,
        parts: [],
      }),
    );
    let server: Job[] = [];
    postJob.mockImplementation(async (body: JobDraft) => {
      const job = await take(body);
      server = [...server, job];
      return job;
    });
    getJobs.mockImplementation(async () => server);
    dismissJob.mockResolvedValue(undefined);
    window.history.pushState({}, "", "/c/ec_1/mb_inbox/m_1");
    const { client } = mount(
      <>
        <Routed />
        <Jobs />
        <Notice />
      </>,
    );

    await moveOpen("m_1");
    await waitFor(() =>
      expect(window.location.pathname).toBe("/c/ec_1/mb_inbox/m_2"),
    );
    await moveOpen("m_2");
    await waitFor(() =>
      expect(window.location.pathname).toBe("/c/ec_1/mb_inbox/m_3"),
    );
    await waitFor(() =>
      expect(
        postJob.mock.calls
          .map((c) => c[0] as JobDraft)
          .filter((j) => j.kind === "move")
          .map((j) => j.message),
      ).toEqual(["m_1", "m_2"]),
    );
    expect(subjectsListed().join()).not.toMatch(/First|Second/);
    screen.getByRole("status");

    // The server did the first move and refused the second.
    const [first, second] = server.filter((j) => j.kind === "move");
    server = server
      .filter((j) => j.id !== first!.id && j.kind === "move")
      .map((j) =>
        j.id === second!.id ? { ...j, error: "The server refused: no." } : j,
      );
    await client.invalidateQueries({ queryKey: qk.jobs });

    await waitFor(() => expect(subjectsListed().join()).toMatch(/Second/));
    expect(subjectsListed().join()).not.toMatch(/First/);
    within(screen.getByRole("alert")).getByText(
      "“Open m_2” was not moved: The server refused: no.",
    );
    expect(dismissJob).toHaveBeenCalledWith(second!.id);
  });

  // Its row in the list is enough to draw who, what and when, and to act on it.
  it("acts on a message before the whole of it has arrived", async () => {
    three();
    getMessage.mockReturnValue(new Promise(() => {}));
    postJob.mockImplementation(take);
    mount(<Mail named="ec_1" mailbox="mb_inbox" message="m_2" />);
    const reader = await screen.findByRole("article", { name: "Message" });
    await within(reader).findByRole("heading", { name: "Second" });
    const picker =
      await within(reader).findByLabelText<HTMLSelectElement>("Move to folder");
    expect(picker.disabled).toBe(false);
    fireEvent.change(picker, { target: { value: "mb_work" } });
    await waitFor(() =>
      expect(window.location.pathname).toBe("/c/ec_1/mb_inbox/m_3"),
    );
  });

  describe("selecting", () => {
    // A Shift-click takes every row from the last one clicked; the selection moved is one
    // request of a job each.
    it("selects a range and moves it in one request", async () => {
      three();
      postJob.mockImplementation(take);
      const list = await start();
      fireEvent.click(checkbox(list, "First").parentElement!);
      fireEvent.click(checkbox(list, "Third"), { shiftKey: true });
      expect(checkbox(list, "Second").checked).toBe(true);
      expect(
        screen.getByRole("checkbox", { name: "Select all" }).closest("label")!
          .textContent,
      ).toBe("3 selected");

      fireEvent.change(screen.getByLabelText("Move to folder"), {
        target: { value: "mb_work" },
      });
      await waitFor(() => expect(postJobsRequest).toHaveBeenCalledTimes(1));
      expect(
        (postJobsRequest.mock.calls[0]![0] as JobDraft[]).map((j) => [
          j.message,
          j.kind,
          j.target,
        ]),
      ).toEqual([
        ["m_1", "move", "mb_work"],
        ["m_2", "move", "mb_work"],
        ["m_3", "move", "mb_work"],
      ]);
      await screen.findByText("No messages here.");
      expect(selectedCount()).toBe("0 selected");
      // The rows that had focus are gone; the selection's own box has it.
      expect(document.activeElement).toBe(
        screen.getByRole("checkbox", { name: "Select all" }),
      );
    });

    // Read if any is unread: only the unread ones are asked about.
    it("marks a mixed selection read, asking only for the unread", async () => {
      getMessages.mockResolvedValue({
        messages: [
          message("m_1", "First", { seen: false }),
          message("m_2", "Second"),
        ],
      });
      postJob.mockImplementation(take);
      const list = await start();
      fireEvent.click(screen.getByRole("checkbox", { name: "Select all" }));
      expect(
        checkbox(list, "First").checked && checkbox(list, "Second").checked,
      ).toBe(true);
      fireEvent.click(screen.getByRole("button", { name: "Mark as read" }));
      await waitFor(() => expect(postJobsRequest).toHaveBeenCalledTimes(1));
      expect(postJobsRequest.mock.calls[0]![0]).toEqual([
        expect.objectContaining({ message: "m_1", kind: "seen", value: true }),
      ]);
    });

    // Every row shown taken away is not an empty folder: what is further back loads.
    it("goes on to older messages once all those shown are moved", async () => {
      getMessages.mockImplementation(
        async (_c: string, _m: string, cursor: string) =>
          cursor === ""
            ? {
                messages: [message("m_1", "First"), message("m_2", "Second")],
                next_cursor: "c1",
              }
            : { messages: [message("m_3", "Third")] },
      );
      postJob.mockImplementation(take);
      await start();
      fireEvent.click(screen.getByRole("checkbox", { name: "Select all" }));
      fireEvent.change(screen.getByLabelText("Move to folder"), {
        target: { value: "mb_work" },
      });
      await waitFor(() =>
        expect(screen.queryByRole("list", { name: "Messages" })).toBeNull(),
      );
      expect(screen.queryByText("No messages here.")).toBeNull();
      screen.getByText("Loading older messages.");

      act(reach);
      const list = within(
        await screen.findByRole("list", { name: "Messages" }),
      );
      expect(checkbox(list, "Third").checked).toBe(false);
      expect(getMessages).toHaveBeenLastCalledWith(
        "ec_1",
        "mb_inbox",
        "c1",
        "",
      );
    });

    // The box itself takes a tap as the row does: once, and it shows what is selected.
    it("toggles a row by its checkbox as by the row", async () => {
      three();
      const list = await start();
      expect(document.activeElement).toBe(checkbox(list, "First"));
      const tick = checkbox(list, "Second");
      fireEvent.click(tick);
      expect(tick.checked).toBe(true);
      expect(selectedCount()).toBe("1 selected");
      fireEvent.click(tick);
      expect(tick.checked).toBe(false);
      expect(selectedCount()).toBe("0 selected");
      fireEvent.click(tick.parentElement!);
      expect(tick.checked).toBe(true);
      expect(selectedCount()).toBe("1 selected");
    });

    it("leaves on Escape, and the rows open messages again", async () => {
      three();
      const list = await start();
      fireEvent.keyDown(document.body, { key: "Escape" });
      expect(document.activeElement).toBe(
        await screen.findByRole("button", { name: "Select messages" }),
      );
      expect(list.queryAllByRole("checkbox")).toHaveLength(0);
      expect(list.getAllByRole("link")).toHaveLength(3);
    });
  });

  // The next run starts loading 200px before the end of the list, measured on the list itself.
  it("reaches further back before the end comes into view", async () => {
    getMessages.mockResolvedValue({
      messages: [message("m_1", "Newer")],
      next_cursor: "c1",
    });
    const seen: IntersectionObserverInit[] = [];
    const real = globalThis.IntersectionObserver;
    globalThis.IntersectionObserver = class {
      constructor(
        _: IntersectionObserverCallback,
        init: IntersectionObserverInit,
      ) {
        seen.push(init);
      }
      observe() {}
      disconnect() {}
    } as unknown as typeof IntersectionObserver;
    try {
      mount(<Mail named="ec_1" mailbox={null} message={null} />);
      const list = await screen.findByRole("list", { name: "Messages" });
      await waitFor(() => expect(seen.length).toBeGreaterThan(0));
      expect(seen.at(-1)!.rootMargin).toBe("0px 0px 200px 0px");
      expect(seen.at(-1)!.root).toBe(list.parentElement);
    } finally {
      globalThis.IntersectionObserver = real;
    }
  });
});
