import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { EmailConfig } from "@app/api/types";
import { Settings } from "@app/islands/app/Settings";
import { mount } from "@app/test/harness";

const getEmailConfigs = vi.fn();
const deleteEmailConfigsById = vi.fn();
const postEmailConfigsByIdTest = vi.fn();
const putArchive = vi.fn();
const putNotifications = vi.fn();
const putOrder = vi.fn();
const getDevices = vi.fn();
const deleteDevicesById = vi.fn();
const postDevicesTest = vi.fn();

vi.mock("@app/api/actions/auth", () => ({
  getAuthMe: async () => ({ id: "u_1", username: "misha", created_at: 0 }),
  postAuthLogout: async () => undefined,
}));
vi.mock("@app/leave", () => ({ leaveFor: vi.fn() }));
vi.mock("@app/api/actions/push", () => ({
  getDevices: () => getDevices(),
  deleteDevicesById: (id: string) => deleteDevicesById(id),
  postDevicesTest: () => postDevicesTest(),
  getPushKey: vi.fn(),
  postDevices: vi.fn(),
}));
vi.mock("@app/api/actions/emailConfigs", () => ({
  getEmailConfigs: () => getEmailConfigs(),
  deleteEmailConfigsById: (id: string) => deleteEmailConfigsById(id),
  postEmailConfigsByIdTest: (id: string, body: unknown) =>
    postEmailConfigsByIdTest(id, body),
  getEmailConfigsByIdMailboxes: async () => [
    {
      id: "mb_inbox",
      name: "INBOX",
      path: ["INBOX"],
      special_use: "inbox",
      selectable: true,
      messages: 1,
      unseen: 0,
      uid_next: 2,
      notify: true,
    },
    {
      id: "mb_trash",
      name: "Trash",
      path: ["Trash"],
      special_use: "trash",
      selectable: true,
      messages: 1,
      unseen: 0,
      uid_next: 2,
      notify: false,
    },
    {
      id: "mb_old",
      name: "Old",
      path: ["Old"],
      special_use: "",
      selectable: true,
      messages: 1,
      unseen: 0,
      uid_next: 2,
      notify: true,
    },
  ],
  putEmailConfigsByIdArchive: (id: string, mailbox: string) =>
    putArchive(id, mailbox),
  putEmailConfigsByIdNotifications: (id: string, body: unknown) =>
    putNotifications(id, body),
  putEmailConfigsOrder: (ids: string[]) => putOrder(ids),
}));

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
    username: "misha@example.com",
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
};

/** The accounts' names, top to bottom. */
const names = () =>
  screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent);

/** An answer given when the test says. */
function later<T>() {
  const out = {} as {
    promise: Promise<T>;
    give: (value: T) => void;
    refuse: (err: Error) => void;
  };
  out.promise = new Promise<T>((give, refuse) => {
    out.give = give;
    out.refuse = refuse;
  });
  return out;
}

beforeEach(() => {
  getEmailConfigs.mockResolvedValue([work]);
  getDevices.mockResolvedValue([]);
});
afterEach(() => vi.clearAllMocks());

describe("settings", () => {
  // A server that names no archive is one to choose for, from folders that keep mail — not Trash —
  // and the choice is saved as it is made.
  it("chooses where a config archives to", async () => {
    putArchive.mockResolvedValue({ ...work, archive_mailbox: "mb_old" });
    mount(<Settings editing={null} />);
    const choice = await screen.findByRole<HTMLSelectElement>("combobox", {
      name: "Archive to",
    });
    expect([...choice.options].map((o) => o.textContent)).toEqual([
      "Automatic: the server names none",
      "Old",
    ]);
    fireEvent.change(choice, { target: { value: "mb_old" } });
    await waitFor(() =>
      expect(putArchive).toHaveBeenCalledWith("ec_1", "mb_old"),
    );
    await waitFor(() => expect(choice.value).toBe("mb_old"));
  });

  it("lists each email config with its servers", async () => {
    mount(<Settings editing={null} />);
    await screen.findByText("Work");
    screen.getByText("misha@example.com");
    screen.getByText("IMAP · imap.example.com:993 · TLS");
    screen.getByText("None");
  });

  it("moves a mail account up or down, in its new place at once", async () => {
    const home = { ...work, id: "ec_2", name: "Home" };
    const shop = { ...work, id: "ec_3", name: "Shop" };
    getEmailConfigs.mockResolvedValue([work, home, shop]);
    const moved = later<EmailConfig[]>();
    putOrder.mockReturnValue(moved.promise);
    mount(<Settings editing={null} />);
    await screen.findByText("Shop");
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Move Work up" })
        .disabled,
    ).toBe(true);
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Move Shop down" })
        .disabled,
    ).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Move Shop up" }));
    await waitFor(() =>
      expect(putOrder).toHaveBeenCalledWith(["ec_1", "ec_3", "ec_2"]),
    );
    await waitFor(() => expect(names()).toEqual(["Work", "Shop", "Home"]));
    moved.give([work, shop, home]);

    // Refused, it goes back to where it was.
    const refused = later<EmailConfig[]>();
    putOrder.mockReturnValue(refused.promise);
    fireEvent.click(screen.getByRole("button", { name: "Move Work down" }));
    await waitFor(() => expect(names()).toEqual(["Shop", "Work", "Home"]));
    refused.refuse(new Error("no"));
    await waitFor(() => expect(names()).toEqual(["Work", "Shop", "Home"]));
  });

  it("offers no moving for a single mail account", async () => {
    mount(<Settings editing={null} />);
    await screen.findByText("Work");
    expect(screen.queryByRole("button", { name: /^Move / })).toBeNull();
  });

  it("says who is signed in and offers a way out", async () => {
    mount(<Settings editing={null} />);
    await screen.findByText("misha");
    screen.getByRole("button", { name: "Sign out" });
  });

  // Over the list rather than in its place, and the address says which.
  it("edits a config in a dialog over the list", async () => {
    mount(<Settings editing={{ id: "ec_1" }} />);
    // The dialog that waits for the list is a different one from the form's.
    await screen.findByDisplayValue("imap.example.com");
    const dialog = screen.getByRole("dialog");
    within(dialog).getByRole("heading", { name: "Edit Work" });
    screen.getByRole("heading", { name: "Work" });
  });

  it("says there are none rather than showing an empty list", async () => {
    getEmailConfigs.mockResolvedValue([]);
    mount(<Settings editing={null} />);
    await screen.findByText(/None yet/);
  });

  // A button that stops to ask ends in an ellipsis, and the asking names what goes.
  it("asks before deleting, and names what it deletes", async () => {
    deleteEmailConfigsById.mockResolvedValue(undefined);
    mount(<Settings editing={null} />);
    fireEvent.click(await screen.findByRole("button", { name: "Delete…" }));
    expect(deleteEmailConfigsById).not.toHaveBeenCalled();
    screen.getByText("Delete Work?");

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() =>
      expect(deleteEmailConfigsById).toHaveBeenCalledWith("ec_1"),
    );
  });

  it("tests a saved config with its saved passwords", async () => {
    postEmailConfigsByIdTest.mockResolvedValue({
      incoming: {
        ok: false,
        message: "imap.example.com:993 refused the connection.",
      },
      outgoing: null,
    });
    mount(<Settings editing={null} />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Test connection" }),
    );
    await screen.findByText(
      "Incoming: imap.example.com:993 refused the connection.",
    );
    const [id, body] = postEmailConfigsByIdTest.mock.calls[0]!;
    expect(id).toBe("ec_1");
    expect(body.incoming.password).toBe("");
  });

  // One choice for how often, and the folders it is for, saved together.
  it("chooses how a config's new mail is told, and from which folders", async () => {
    putNotifications.mockResolvedValue({
      ...work,
      notify: "digest",
      digest_every: 120,
    });
    mount(<Settings editing={null} />);
    await screen.findByText("As mail arrives, from Inbox and 1 folder");
    fireEvent.click(
      screen.getByRole("button", { name: "Change notifications" }),
    );
    const dialog = within(await screen.findByRole("dialog"));
    expect(
      dialog
        .getAllByRole<HTMLInputElement>("checkbox")
        .map((box) => `${box.parentElement!.textContent} ${box.checked}`),
    ).toEqual(["Inbox true", "Trash false", "Old true"]);
    fireEvent.change(dialog.getByRole("combobox", { name: "New mail" }), {
      target: { value: "120" },
    });
    fireEvent.click(dialog.getByRole("checkbox", { name: "Old" }));
    fireEvent.click(dialog.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(putNotifications).toHaveBeenCalledWith("ec_1", {
        notify: "digest",
        digest_every: 120,
        mailboxes: { mb_old: false },
      }),
    );
    await screen.findByText("Every 2 hours, from Inbox");
  });

  // A browser without push still lists the devices that have it, to remove or to test.
  it("lists the devices notified, to remove or send a test to", async () => {
    getDevices.mockResolvedValue([
      {
        id: "dv_1",
        client_id: "elsewhere",
        label: "Safari on iPhone",
        created_at: 0,
        last_ok_at: null,
        failure_count: 1,
        last_error: "push busy (429): slow down",
      },
    ]);
    postDevicesTest.mockResolvedValue({ delivered: 1 });
    deleteDevicesById.mockResolvedValue(undefined);
    mount(<Settings editing={null} />);
    await screen.findByText("This browser cannot show notifications.");
    const devices = within(
      await screen.findByRole("list", { name: "Devices" }),
    );
    devices.getByText("Safari on iPhone");
    devices.getByText(/slow down/);

    fireEvent.click(screen.getByRole("button", { name: "Send a test" }));
    await screen.findByText("Sent to one device.");
    fireEvent.click(devices.getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(deleteDevicesById).toHaveBeenCalledWith("dv_1"));
  });
});
