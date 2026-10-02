import {
  act,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { qk } from "@app/api/keys";
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
  ) => getMessages(id, mailbox, cursor),
  postEmailConfigsByIdSync: (id: string) => postEmailConfigsByIdSync(id),
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
    <Mail named={r.config} mailbox={r.mailbox} message={r.message} />
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
const checkbox = (list: ReturnType<typeof within>, name: string) =>
  list.getByRole("checkbox", { name }) as HTMLInputElement;

describe("the mail view", () => {
  it("opens on the inbox and lists its messages", async () => {
    mount(<Mail named="ec_1" mailbox={null} message={null} />);
    const list = await screen.findByRole("list", { name: "Messages" });
    expect(getMessages).toHaveBeenCalledWith("ec_1", "mb_inbox", "");
    const rows = within(list).getAllByRole("listitem");
    expect(rows).toHaveLength(2);
    within(rows[0]!).getByText("Unread.");
    within(rows[0]!).getByText("Quarterly numbers");
    within(rows[0]!).getByRole("img", { name: "Attachment" });
    within(rows[1]!).getByRole("img", { name: "Flagged" });
    expect(within(rows[1]!).queryByText("Unread.")).toBeNull();
  });

  it("lists folders in the server's tree, with what is unread", async () => {
    mount(<Mail named="ec_1" mailbox={null} message={null} />);
    const nav = await screen.findByRole("navigation", { name: "Folders" });
    const links = await within(nav).findAllByRole("link");
    expect(links.map((l) => l.textContent)).toEqual([
      "Inbox, unread: 2",
      "Sent",
      "Work",
      "Clients, unread: 1",
    ]);
    expect(links[0]!.getAttribute("aria-current")).toBe("page");
  });

  it("opens another folder from the sidebar, and the address says which", async () => {
    mount(<Mail named="ec_1" mailbox={null} message={null} />);
    const nav = await screen.findByRole("navigation", { name: "Folders" });
    fireEvent.click(await within(nav).findByRole("link", { name: /Clients/ }));
    expect(window.location.pathname).toBe("/c/ec_1/mb_clients");
  });

  // In mail somebody sent, the useful name is who it went to.
  it("shows who sent mail went to", async () => {
    mount(<Mail named="ec_1" mailbox="mb_sent" message={null} />);
    await screen.findAllByText("To: Bob");
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
    expect(getMessages).toHaveBeenLastCalledWith("ec_1", "mb_inbox", "c1");
    expect(screen.queryByText("Loading older messages.")).toBeNull();
  });

  it("asks for a look now", async () => {
    mount(<Mail named="ec_1" mailbox={null} message={null} />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Fetch new mail" }),
    );
    await waitFor(() =>
      expect(postEmailConfigsByIdSync).toHaveBeenCalledWith("ec_1"),
    );
    screen.getByText(/Updated 2 minutes ago/);
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
      fireEvent.click(checkbox(list, "First").closest("label")!);
      fireEvent.click(checkbox(list, "Third").closest("label")!, {
        shiftKey: true,
      });
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
      expect(
        screen.getByRole("checkbox", { name: "Select all" }).closest("label")!
          .textContent,
      ).toBe("0 selected");
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
      expect(getMessages).toHaveBeenLastCalledWith("ec_1", "mb_inbox", "c1");
    });

    it("leaves on Escape, and the rows open messages again", async () => {
      three();
      const list = await start();
      fireEvent.keyDown(document.body, { key: "Escape" });
      await screen.findByRole("button", { name: "Select messages" });
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
