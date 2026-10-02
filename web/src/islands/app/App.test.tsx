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

  it("signs out and goes to the sign-in page", async () => {
    postAuthLogout.mockResolvedValue(undefined);
    mount(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await waitFor(() => expect(leaveFor).toHaveBeenCalledWith("/login"));
  });

  it("points somewhere when there are no email configs yet", async () => {
    mount(<App />);
    fireEvent.click(
      await screen.findByRole("link", { name: "Add an email config" }),
    );
    expect(window.location.pathname).toBe("/settings/email-configs/new");
    const dialog = await screen.findByRole("dialog");
    within(dialog).getByRole("heading", { name: "Add email config" });
  });

  // One config at a time, chosen in the header, and the address says which.
  it("switches email config from the header", async () => {
    getEmailConfigs.mockResolvedValue([
      config("ec_1", "Work"),
      config("ec_2", "Home"),
    ]);
    mount(<App />);
    await screen.findByRole("heading", { name: "Inbox of ec_1" });

    fireEvent.change(screen.getByLabelText("Email config"), {
      target: { value: "ec_2" },
    });
    expect(window.location.pathname).toBe("/c/ec_2");
    await screen.findByRole("heading", { name: "Inbox of ec_2" });
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
