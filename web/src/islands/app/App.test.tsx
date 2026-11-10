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
  inbox_unseen: 0,
  archive_mailbox: null,
  notify: "now",
  digest_every: 60,
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

  // One config at a time, chosen in the header from a list that counts the unread in each Inbox,
  // and the address says which.
  it("switches mail account from the header", async () => {
    getEmailConfigs.mockResolvedValue([
      config("ec_1", "Work"),
      { ...config("ec_2", "Home"), inbox_unseen: 2 },
    ]);
    mount(<App />);
    await screen.findByRole("heading", { name: "Inbox of ec_1" });
    fireEvent.click(
      screen.getByRole("button", { name: /^Mail account:\s*Work/ }),
    );
    const menu = within(await screen.findByRole("menu"));
    expect(menu.getAllByRole("menuitem").map((l) => l.textContent)).toEqual([
      "Workwork@example.com",
      "Homehome@example.com, unread in Inbox: 2",
      "Add mail account",
    ]);
    fireEvent.click(menu.getByRole("menuitem", { name: /^Home/ }));
    expect(window.location.pathname).toBe("/c/ec_2");
    await screen.findByRole("heading", { name: "Inbox of ec_2" });
    expect(screen.queryByRole("menu")).toBeNull();
  });

  // Side by side where they fit, which jsdom, measuring nothing, says they always do.
  it("puts the mail accounts side by side where they fit", async () => {
    getEmailConfigs.mockResolvedValue([
      config("ec_1", "Work"),
      { ...config("ec_2", "Home"), inbox_unseen: 2 },
    ]);
    const real = globalThis.ResizeObserver;
    globalThis.ResizeObserver = class {
      constructor(private measure: () => void) {}
      observe() {
        this.measure();
      }
      unobserve() {}
      disconnect() {}
    } as unknown as typeof ResizeObserver;
    try {
      mount(<App />);
      await screen.findByRole("heading", { name: "Inbox of ec_1" });
      const row = within(
        await screen.findByRole("navigation", { name: "Mail accounts" }),
      );
      expect(
        row.getAllByRole("link").map((l) => [l.textContent, l.ariaCurrent]),
      ).toEqual([
        ["Work", "page"],
        ["Home, unread in Inbox: 2", null],
      ]);
      expect(
        screen.queryByRole("button", { name: /^Mail account/ }),
      ).toBeNull();
      fireEvent.click(row.getByRole("link", { name: /^Home/ }));
      await screen.findByRole("heading", { name: "Inbox of ec_2" });
    } finally {
      globalThis.ResizeObserver = real;
    }
  });

  // The open one's unread are counted beside its folders already.
  it("marks the header while another mail account has unread mail", async () => {
    getEmailConfigs.mockResolvedValue([
      { ...config("ec_1", "Work"), inbox_unseen: 3 },
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
