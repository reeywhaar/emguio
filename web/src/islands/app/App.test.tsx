import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { EmailConfig } from "@app/api/types";
import { App } from "@app/islands/app/App";
import { mount } from "@app/test/harness";

const getAuthMe = vi.fn();
const postAuthLogout = vi.fn();
const getEmailConfigs = vi.fn();
const getEmailConfigsByIdMailboxes = vi.fn();
const getMessages = vi.fn();
const leaveFor = vi.fn();

vi.mock("@app/api/actions/auth", () => ({
  getAuthMe: () => getAuthMe(),
  postAuthLogout: () => postAuthLogout(),
}));
vi.mock("@app/api/actions/emailConfigs", () => ({
  getEmailConfigs: () => getEmailConfigs(),
  getEmailConfigsByIdMailboxes: (id: string) =>
    getEmailConfigsByIdMailboxes(id),
  getEmailConfigsByIdMailboxesByMailboxMessages: (
    id: string,
    mailbox: string,
    cursor: string,
  ) => getMessages(id, mailbox, cursor),
  postEmailConfigsByIdSync: vi.fn(),
  putEmailConfigsOrder: vi.fn(),
}));
vi.mock("@app/leave", () => ({ leaveFor: (path: string) => leaveFor(path) }));

const config = (id: string, name: string): EmailConfig => ({
  id,
  name,
  email: `${name.toLowerCase()}@example.com`,
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
  synced_at: null,
  sync_error: "",
  unseen: 0,
  archive_mailbox: null,
  notify: "now",
  digest_every: 60,
  off: false,
  read_on_archive: null,
  read_on_trash: null,
});

beforeEach(() => {
  window.history.pushState({}, "", "/");
  window.localStorage.clear();
  getAuthMe.mockResolvedValue({ id: "u_1", username: "misha", created_at: 0 });
  getEmailConfigs.mockResolvedValue([]);
  // Each config's INBOX is named for it, so a test can tell which one is open.
  getEmailConfigsByIdMailboxes.mockImplementation(async (id: string) => [
    {
      id: `mb_${id}`,
      name: `INBOX`,
      path: [`Inbox of ${id}`],
      special_use: "",
      selectable: true,
      messages: 0,
      unseen: 0,
      uid_next: 1,
      notify: true,
    },
  ]);
  getMessages.mockResolvedValue({ messages: [] });
});

afterEach(() => vi.clearAllMocks());

/**
 * Sizes for what jsdom does not measure: each mail account 100 wide in the header, the button for
 * the rest 30, and the room for them all as given. Undone by what it returns.
 */
function measuring(room: number) {
  const observer = globalThis.ResizeObserver;
  const rect = HTMLElement.prototype.getBoundingClientRect;
  globalThis.ResizeObserver = class {
    constructor(private measure: () => void) {}
    observe() {
      this.measure();
    }
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
  HTMLElement.prototype.getBoundingClientRect = function () {
    const width = this.dataset.config
      ? 100
      : this.dataset.rest !== undefined
        ? 30
        : room;
    return { width } as DOMRect;
  };
  return () => {
    globalThis.ResizeObserver = observer;
    HTMLElement.prototype.getBoundingClientRect = rect;
  };
}

const row = () =>
  within(screen.getByRole("navigation", { name: "Mail accounts" }));

/** What the header's row of mail accounts says, each link and button in turn. */
const names = () =>
  [
    ...screen
      .getByRole("navigation", { name: "Mail accounts" })
      .querySelectorAll("a, button"),
  ].map((el) => el.textContent!.replace(/\s+/g, " ").trim());

describe("the application", () => {
  it("says who is signed in", async () => {
    mount(<App />);
    await screen.findByText("misha");
  });

  it("signs out from Settings and goes to the sign-in page", async () => {
    postAuthLogout.mockResolvedValue(undefined);
    mount(<App />);
    await screen.findByText("misha");
    expect(screen.queryByRole("button", { name: "Sign out" })).toBeNull();
    fireEvent.click(
      screen.getByRole("link", { name: "Settings, signed in as misha" }),
    );
    fireEvent.click(await screen.findByRole("button", { name: "Sign out" }));
    await waitFor(() => expect(leaveFor).toHaveBeenCalledWith("/login"));
  });

  it("points somewhere when there are no email configs yet", async () => {
    mount(<App />);
    fireEvent.click(
      await screen.findByRole("link", { name: "Add a mail account" }),
    );
    expect(window.location.pathname).toBe("/settings/email-configs/new");
    const dialog = await screen.findByRole("dialog");
    within(dialog).getByRole("heading", { name: "Add mail account" });
  });

  // Where the accounts would be, while there are none.
  it("offers to add a mail account in the header while there are none", async () => {
    mount(<App />);
    fireEvent.click(
      await screen.findByRole("link", { name: "Add mail account" }),
    );
    expect(window.location.pathname).toBe("/settings/email-configs/new");
  });

  // One config at a time, chosen in the header from a list that counts the unread in each, and the
  // address says which.
  it("switches mail account from the header", async () => {
    getEmailConfigs.mockResolvedValue([
      config("ec_1", "Work"),
      { ...config("ec_2", "Home"), unseen: 2 },
    ]);
    mount(<App />);
    await screen.findByRole("heading", { name: "Inbox of ec_1" });
    fireEvent.click(
      screen.getByRole("button", { name: /^Mail account:\s*Work/ }),
    );
    const menu = within(await screen.findByRole("menu"));
    expect(menu.getAllByRole("menuitem").map((l) => l.textContent)).toEqual([
      "Workwork@example.com",
      "Homehome@example.com, unread: 2",
    ]);
    fireEvent.click(menu.getByRole("menuitem", { name: /^Home/ }));
    expect(window.location.pathname).toBe("/c/ec_2");
    await screen.findByRole("heading", { name: "Inbox of ec_2" });
    expect(screen.queryByRole("menu")).toBeNull();
  });

  // Side by side where they fit; the open one lists them all.
  it("puts the mail accounts side by side where they fit", async () => {
    getEmailConfigs.mockResolvedValue([
      config("ec_1", "Work"),
      { ...config("ec_2", "Home"), unseen: 2 },
    ]);
    const restore = measuring(1000);
    try {
      mount(<App />);
      await screen.findByRole("heading", { name: "Inbox of ec_1" });
      await screen.findByRole("navigation", { name: "Mail accounts" });
      expect(names()).toEqual(["Mail account: Work", "Home, unread: 2"]);
      expect(
        screen.queryByRole("button", { name: /^Other mail accounts/ }),
      ).toBeNull();
      fireEvent.click(row().getByRole("link", { name: /^Home/ }));
      await screen.findByRole("heading", { name: "Inbox of ec_2" });
      expect(names()).toEqual(["Work", "Mail account: Home, unread: 2"]);

      fireEvent.click(row().getByRole("button", { name: /^Mail account/ }));
      const menu = within(await screen.findByRole("menu"));
      expect(menu.getAllByRole("menuitem").map((l) => l.textContent)).toEqual([
        "Workwork@example.com",
        "Homehome@example.com, unread: 2",
      ]);
      fireEvent.click(menu.getByRole("menuitem", { name: /^Work/ }));
      await screen.findByRole("heading", { name: "Inbox of ec_1" });
    } finally {
      restore();
    }
  });

  // Settings has no account open: each one in the row goes to its mail, a lone one too.
  it("goes to an account's mail from the header in Settings", async () => {
    getEmailConfigs.mockResolvedValue([config("ec_1", "Work")]);
    const restore = measuring(1000);
    try {
      window.history.replaceState(null, "", "/settings");
      mount(<App />);
      await screen.findByRole("navigation", { name: "Mail accounts" });
      expect(names()).toEqual(["Work"]);
      fireEvent.click(row().getByRole("link", { name: "Work" }));
      await screen.findByRole("heading", { name: "Inbox of ec_1" });
      screen.getByRole("button", { name: /^Mail account:\s*Work$/ });
    } finally {
      restore();
    }
  });

  // The open one and as many as fit beside a button for the rest; the open one stays in the row.
  it("puts the mail accounts that do not fit behind a button", async () => {
    getEmailConfigs.mockResolvedValue([
      config("ec_1", "Work"),
      config("ec_2", "Home"),
      { ...config("ec_3", "Shop"), unseen: 2 },
      config("ec_4", "Club"),
    ]);
    // Two of them beside the button.
    const restore = measuring(250);
    try {
      mount(<App />);
      await screen.findByRole("heading", { name: "Inbox of ec_1" });
      await waitFor(() =>
        expect(names()).toEqual([
          "Mail account: Work",
          "Home",
          "Other mail accounts: , unread in one",
        ]),
      );
      fireEvent.click(
        row().getByRole("button", {
          name: /^Other mail accounts:\s*, unread in one$/,
        }),
      );
      const menu = within(await screen.findByRole("menu"));
      expect(menu.getAllByRole("menuitem").map((l) => l.textContent)).toEqual([
        "Shopshop@example.com, unread: 2",
        "Clubclub@example.com",
      ]);
      fireEvent.click(menu.getByRole("menuitem", { name: /^Shop/ }));
      await screen.findByRole("heading", { name: "Inbox of ec_3" });
      expect(names()).toEqual([
        "Work",
        "Mail account: Shop, unread: 2",
        "Other mail accounts:",
      ]);
    } finally {
      restore();
    }
  });

  it("puts a dot on the tab's icon while any mail account has unread mail", async () => {
    const icon = document.createElement("link");
    icon.rel = "icon";
    icon.href = "/favicon.svg";
    document.head.append(icon);
    try {
      getEmailConfigs.mockResolvedValue([
        config("ec_1", "Work"),
        { ...config("ec_2", "Home"), unseen: 2 },
      ]);
      mount(<App />);
      await waitFor(() =>
        expect(icon.getAttribute("href")).toBe("/favicon-unread.svg"),
      );
    } finally {
      icon.remove();
    }
  });

  it("takes the dot off the tab's icon once nothing is unread", async () => {
    const icon = document.createElement("link");
    icon.rel = "icon";
    icon.href = "/favicon-unread.svg";
    document.head.append(icon);
    try {
      getEmailConfigs.mockResolvedValue([config("ec_1", "Work")]);
      mount(<App />);
      await screen.findByRole("heading", { name: "Inbox of ec_1" });
      await waitFor(() =>
        expect(icon.getAttribute("href")).toBe("/favicon.svg"),
      );
    } finally {
      icon.remove();
    }
  });

  // The open one's unread are counted beside its folders already.
  it("marks the header while another mail account has unread mail", async () => {
    getEmailConfigs.mockResolvedValue([
      { ...config("ec_1", "Work"), unseen: 3 },
      config("ec_2", "Home"),
    ]);
    mount(<App />);
    await screen.findByRole("heading", { name: "Inbox of ec_1" });
    screen.getByRole("button", { name: /^Mail account:\s*Work$/ });

    fireEvent.click(screen.getByRole("button", { name: /^Mail account/ }));
    fireEvent.click(
      within(await screen.findByRole("menu")).getByRole("menuitem", {
        name: /^Home/,
      }),
    );
    await screen.findByRole("heading", { name: "Inbox of ec_2" });
    screen.getByRole("button", {
      name: /^Mail account:\s*Home, unread in another$/,
    });
  });

  it("opens on the config last looked at", async () => {
    getEmailConfigs.mockResolvedValue([
      config("ec_1", "Work"),
      config("ec_2", "Home"),
    ]);
    window.localStorage.setItem("emguio.email-config", "ec_2");
    mount(<App />);
    await screen.findByRole("heading", { name: "Inbox of ec_2" });
  });
});
