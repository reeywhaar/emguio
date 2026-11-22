import {
  act,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@app/api/transport";
import type { EmailConfig } from "@app/api/types";
import { Settings } from "@app/islands/app/Settings";
import { mount } from "@app/test/harness";
import { later } from "@app/test/later";

const getEmailConfigs = vi.fn();
const deleteEmailConfigsById = vi.fn();
const postEmailConfigsByIdTest = vi.fn();
const putArchive = vi.fn();
const putNotifications = vi.fn();
const putOrder = vi.fn();
const putOff = vi.fn();
const getMailboxes = vi.fn();
const getDevices = vi.fn();
const deleteDevicesById = vi.fn();
const postDevicesTest = vi.fn();
const getReading = vi.fn();
const putReading = vi.fn();
const putConfigReading = vi.fn();
const getPushSettings = vi.fn();
const putPushSettings = vi.fn();

vi.mock("@app/api/actions/auth", () => ({
  getAuthMe: async () => ({ id: "u_1", username: "misha", created_at: 0 }),
  postAuthLogout: async () => undefined,
  putAuthPassword: vi.fn(),
}));
vi.mock("@app/leave", () => ({ leaveFor: vi.fn() }));
vi.mock("@app/api/actions/sessions", () => ({
  getSessions: async () => [],
  deleteSessionsById: vi.fn(),
  deleteSessions: vi.fn(),
}));
vi.mock("@app/api/actions/push", () => ({
  getDevices: () => getDevices(),
  deleteDevicesById: (id: string) => deleteDevicesById(id),
  postDevicesTest: () => postDevicesTest(),
  getPushSettings: () => getPushSettings(),
  putPushSettings: (body: unknown) => putPushSettings(body),
  getPushKey: vi.fn(),
  postDevices: vi.fn(),
}));
vi.mock("@app/api/actions/reading", () => ({
  getReading: () => getReading(),
  putReading: (body: unknown) => putReading(body),
}));
vi.mock("@app/api/actions/emailConfigs", () => ({
  putEmailConfigsByIdReading: (id: string, body: unknown) =>
    putConfigReading(id, body),
  getEmailConfigs: () => getEmailConfigs(),
  deleteEmailConfigsById: (id: string) => deleteEmailConfigsById(id),
  postEmailConfigsByIdTest: (id: string, body: unknown) =>
    postEmailConfigsByIdTest(id, body),
  getEmailConfigsByIdMailboxes: () => getMailboxes(),
  putEmailConfigsByIdArchive: (id: string, mailbox: string) =>
    putArchive(id, mailbox),
  putEmailConfigsByIdNotifications: (id: string, body: unknown) =>
    putNotifications(id, body),
  putEmailConfigsOrder: (ids: string[]) => putOrder(ids),
  putEmailConfigsByIdOff: (id: string, off: boolean) => putOff(id, off),
}));

const boxes = [
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
];

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
  unseen: 0,
  archive_mailbox: null,
  notify: "now",
  digest_every: 60,
  off: false,
  read_on_archive: null,
  read_on_trash: null,
};

/** The Change of the Settings section headed so. */
const changeIn = (heading: string) =>
  within(
    screen.getByRole("heading", { name: heading }).closest("section")!,
  ).getByRole("button", { name: "Change" });

/** A paragraph whose whole text is this, however it is split into parts. */
const line = (text: string) =>
  screen.findByText((_, el) => el?.tagName === "P" && el.textContent === text);

/** The accounts' names in the list, top to bottom. */
const names = () =>
  within(screen.getByRole("list", { name: "Mail accounts" }))
    .getAllByRole("button")
    .map((row) => row.textContent!.split(work.email)[0]);

/** A row of the list, by the account's name. */
const row = (name: string) =>
  within(screen.getByRole("list", { name: "Mail accounts" })).getByRole(
    "button",
    { name: new RegExp(`^${name}`) },
  );

beforeEach(() => {
  getEmailConfigs.mockResolvedValue([work]);
  getMailboxes.mockResolvedValue(boxes);
  getDevices.mockResolvedValue([]);
  getPushSettings.mockResolvedValue({ silent: false, quiet: null });
  getReading.mockResolvedValue({
    read_on_archive: false,
    read_on_trash: false,
  });
  putReading.mockImplementation(async (body: unknown) => body);
  putPushSettings.mockImplementation(async (body: unknown) => body);
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
    const open = within(await screen.findByRole("article"));
    open.getByRole("heading", { name: "Work" });
    open.getByText("misha@example.com");
    open.getByText("imap.example.com:993");
    open.getByText("IMAP");
    open.getByText("TLS");
    open.getByText("None");
  });

  it("says in each row what the account has", async () => {
    getEmailConfigs.mockResolvedValue([
      work,
      {
        ...work,
        id: "ec_2",
        name: "Home",
        outgoing: { ...work.incoming, host: "smtp.example.com", port: 465 },
        notify: "off",
      },
    ]);
    mount(<Settings editing={null} />);
    await screen.findByRole("list", { name: "Mail accounts" });
    await within(row("Work")).findByText(
      "IMAP · Notifies as mail arrives, from Inbox and 1 folder",
    );
    within(row("Home")).getByText("IMAP · SMTP · No notifications");
  });

  // Wide, the list and the account picked from it side by side; the first until one is picked.
  it("shows the account picked from the list beside it", async () => {
    getEmailConfigs.mockResolvedValue([
      work,
      { ...work, id: "ec_2", name: "Home" },
    ]);
    mount(<Settings editing={null} />);
    await screen.findByRole("heading", { name: "Work" });
    expect(row("Work").getAttribute("aria-current")).toBe("true");
    fireEvent.click(row("Home"));
    await screen.findByRole("heading", { name: "Home" });
    expect(row("Home").getAttribute("aria-current")).toBe("true");
    expect(screen.queryByRole("heading", { name: "Work" })).toBeNull();
  });

  // On a phone, an account picked takes the screen, with a close; none is open at first.
  it("opens an account over the list on a phone", async () => {
    const wide = window.innerWidth;
    window.innerWidth = 390;
    try {
      getEmailConfigs.mockResolvedValue([
        work,
        { ...work, id: "ec_2", name: "Home" },
      ]);
      mount(<Settings editing={null} />);
      await screen.findByRole("list", { name: "Mail accounts" });
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(screen.queryByText("imap.example.com:993")).toBeNull();

      fireEvent.click(row("Home"));
      const dialog = within(await screen.findByRole("dialog"));
      dialog.getByRole("heading", { name: "Home" });
      dialog.getByText("imap.example.com:993");

      fireEvent.click(dialog.getByRole("button", { name: "Close" }));
      await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    } finally {
      window.innerWidth = wide;
    }
  });

  describe("dragging an account", () => {
    // Every row is somewhere to land; which one is the test's to say.
    let under: Element | null = null;
    const home = { ...work, id: "ec_2", name: "Home" };
    const shop = { ...work, id: "ec_3", name: "Shop" };
    beforeEach(() => {
      document.elementFromPoint = () => under;
      getEmailConfigs.mockResolvedValue([work, home, shop]);
    });
    const drag = (from: string, to: string, pointerType = "mouse") => {
      const carried = row(from);
      under = row(to);
      fireEvent.pointerDown(carried, {
        button: 0,
        pointerId: 1,
        pointerType,
        clientX: 10,
        clientY: 50,
      });
      fireEvent.pointerMove(carried, {
        pointerId: 1,
        pointerType,
        clientX: 10,
        clientY: 10,
      });
      fireEvent.pointerUp(carried, { pointerId: 1 });
      return carried;
    };

    it("puts it in its new place at once, and does not open it", async () => {
      const moved = later<EmailConfig[]>();
      putOrder.mockReturnValue(moved.promise);
      mount(<Settings editing={null} />);
      await screen.findByText("Shop");
      fireEvent.click(drag("Shop", "Home"));
      await waitFor(() =>
        expect(putOrder).toHaveBeenCalledWith(["ec_1", "ec_3", "ec_2"]),
      );
      expect(names()).toEqual(["Work", "Shop", "Home"]);
      screen.getByRole("heading", { name: "Work" });
      moved.give([work, shop, home]);

      // Down, it goes after the one it is let go on.
      putOrder.mockResolvedValue([shop, home, work]);
      drag("Work", "Home");
      await waitFor(() =>
        expect(putOrder).toHaveBeenLastCalledWith(["ec_3", "ec_2", "ec_1"]),
      );
    });

    it("by finger, once it has rested; one that moves at once is scrolling", async () => {
      putOrder.mockResolvedValue([work, shop, home]);
      mount(<Settings editing={null} />);
      await screen.findByText("Shop");
      drag("Shop", "Home", "touch");
      await act(() => new Promise((done) => setTimeout(done, 20)));
      expect(putOrder).not.toHaveBeenCalled();

      const carried = row("Shop");
      under = row("Home");
      fireEvent.pointerDown(carried, {
        button: 0,
        pointerId: 1,
        pointerType: "touch",
        clientX: 10,
        clientY: 50,
      });
      await act(() => new Promise((done) => setTimeout(done, 450)));
      fireEvent.pointerUp(carried, { pointerId: 1 });
      await waitFor(() =>
        expect(putOrder).toHaveBeenCalledWith(["ec_1", "ec_3", "ec_2"]),
      );
    });

    // A grip does not scroll, so a finger on it carries at once.
    it("by finger on its grip, at once", async () => {
      putOrder.mockResolvedValue([work, shop, home]);
      mount(<Settings editing={null} />);
      await screen.findByText("Shop");
      const carried = row("Shop");
      under = row("Home");
      const grip = carried.querySelector("[data-grip]")!;
      fireEvent.pointerDown(grip, {
        button: 0,
        pointerId: 1,
        pointerType: "touch",
        clientX: 10,
        clientY: 50,
      });
      fireEvent.pointerMove(grip, {
        pointerId: 1,
        pointerType: "touch",
        clientX: 10,
        clientY: 10,
      });
      fireEvent.pointerUp(grip, { pointerId: 1 });
      await waitFor(() =>
        expect(putOrder).toHaveBeenCalledWith(["ec_1", "ec_3", "ec_2"]),
      );
    });

    it("goes back where it was when the server refuses", async () => {
      const refused = later<EmailConfig[]>();
      putOrder.mockReturnValue(refused.promise);
      mount(<Settings editing={null} />);
      await screen.findByText("Shop");
      drag("Work", "Home");
      await waitFor(() => expect(names()).toEqual(["Home", "Work", "Shop"]));
      refused.refuse(new Error("no"));
      await waitFor(() => expect(names()).toEqual(["Work", "Home", "Shop"]));
    });
  });

  // Off, an account is kept and says so, and its switch turns it on again.
  it("turns an account off and on", async () => {
    putOff.mockImplementation(async (_id: string, off: boolean) => ({
      ...work,
      off,
    }));
    mount(<Settings editing={null} />);
    const open = within(await screen.findByRole("article"));
    const power = open.getByRole("switch", { name: "Turned on" });
    expect(power.getAttribute("aria-checked")).toBe("true");
    fireEvent.click(power);
    await waitFor(() => expect(putOff).toHaveBeenCalledWith("ec_1", true));
    await open.findByText(/Turned off: emguio does not read this account/);
    within(row("Work")).getByText("Off");
    expect(power.getAttribute("aria-checked")).toBe("false");

    await waitFor(() => expect(power.hasAttribute("disabled")).toBe(false));
    fireEvent.click(power);
    await waitFor(() => expect(putOff).toHaveBeenLastCalledWith("ec_1", false));
    await waitFor(() =>
      expect(within(row("Work")).queryByText("Off")).toBeNull(),
    );
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
    const dialog = within(await screen.findByRole("dialog"));
    dialog.getByRole("heading", { name: "Delete Work?" });
    dialog.getByText(/Nothing on the mail server changes/);

    fireEvent.click(dialog.getByRole("button", { name: "Delete" }));
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

  // How often, saved as it is chosen, and the folders, in a dialog of their own.
  it("chooses how a config's new mail is told, and from which folders", async () => {
    putNotifications.mockResolvedValue({
      ...work,
      notify: "digest",
      digest_every: 120,
    });
    mount(<Settings editing={null} />);
    const open = within(await screen.findByRole("article"));
    const often = await open.findByRole<HTMLSelectElement>("combobox", {
      name: "Notify",
    });
    expect(often.value).toBe("now");

    // How often is saved as it is chosen.
    fireEvent.change(often, { target: { value: "120" } });
    await waitFor(() =>
      expect(putNotifications).toHaveBeenCalledWith("ec_1", {
        notify: "digest",
        digest_every: 120,
        mailboxes: {},
      }),
    );
    await waitFor(() => expect(often.value).toBe("120"));

    // The folders, in a dialog of their own, every one of which can be left out.
    fireEvent.click(open.getByRole("button", { name: "Inbox and 1 folder" }));
    const dialog = within(await screen.findByRole("dialog"));
    expect(
      dialog
        .getAllByRole<HTMLInputElement>("checkbox")
        .map((box) => `${box.parentElement!.textContent} ${box.checked}`),
    ).toEqual(["Inbox true", "Trash false", "Old true"]);
    fireEvent.click(dialog.getByRole("checkbox", { name: "Inbox" }));
    fireEvent.click(dialog.getByRole("checkbox", { name: "Old" }));
    fireEvent.click(dialog.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(putNotifications).toHaveBeenLastCalledWith("ec_1", {
        notify: "digest",
        digest_every: 0,
        mailboxes: { mb_inbox: false, mb_old: false },
      }),
    );
    await open.findByRole("button", { name: "no folders" });
  });

  // An empty dialog would say nothing; it says why there is nothing to tick.
  it("says when there are no folders to notify from, or they would not load", async () => {
    getEmailConfigs.mockResolvedValue([{ ...work, synced_at: 1 }]);
    getMailboxes.mockResolvedValue([]);
    const { unmount } = mount(<Settings editing={null} />);
    fireEvent.click(await screen.findByRole("button", { name: "no folders" }));
    let dialog = within(await screen.findByRole("dialog"));
    dialog.getByText("No folders.");
    expect(
      dialog.getByRole<HTMLButtonElement>("button", { name: "Save" }).disabled,
    ).toBe(true);
    unmount();

    getMailboxes.mockRejectedValue(
      new ApiError(0, "unreachable", "The server could not be reached."),
    );
    mount(<Settings editing={null} />);
    fireEvent.click(await screen.findByRole("button", { name: "folders" }));
    dialog = within(await screen.findByRole("dialog"));
    dialog.getByText(
      "Unable to load folders. The server could not be reached.",
    );
  });

  // Unread is not none: while the account cannot be read its folders are unknown, and why is
  // said where it is open.
  it("says why an account that cannot be read has no folders", async () => {
    const refused = "imap.example.com:993 refused the connection.";
    getEmailConfigs.mockResolvedValue([
      { ...work, synced_at: 1, sync_error: refused },
    ]);
    getMailboxes.mockResolvedValue([]);
    mount(<Settings editing={null} />);
    const open = within(await screen.findByRole("article"));
    open.getByText(refused);
    within(row("Work")).getByText(", its mail cannot be read");
    fireEvent.click(await open.findByRole("button", { name: "folders" }));
    within(await screen.findByRole("dialog")).getByText(
      `Unable to load folders. ${refused}`,
    );
  });

  // Never needs no folders.
  it("offers no folders to a config that never notifies", async () => {
    getEmailConfigs.mockResolvedValue([{ ...work, notify: "off" }]);
    mount(<Settings editing={null} />);
    const open = within(await screen.findByRole("article"));
    await open.findByRole("combobox", { name: "Notify" });
    expect(open.queryByText(/^from/)).toBeNull();
  });

  it("chooses for every account whether archived and deleted mail is marked read", async () => {
    mount(<Settings editing={null} />);
    const section = within(
      (await screen.findByRole("heading", { name: "Reading" })).closest(
        "section",
      )!,
    );
    await section.findByText("Archiving and deleting leave mail as it is");
    fireEvent.click(section.getByRole("button", { name: "Change" }));
    const dialog = within(await screen.findByRole("form", { name: "Reading" }));
    fireEvent.click(
      dialog.getByRole("switch", { name: "Mark read when deleting" }),
    );
    expect(putReading).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(putReading).toHaveBeenCalledWith({
        read_on_archive: false,
        read_on_trash: true,
      }),
    );
    await section.findByText("Deleted mail is marked read");
  });

  // Its own choice, or as set for all, which the choice says.
  it("lets an account choose otherwise", async () => {
    putConfigReading.mockImplementation(async (_id: string, body: object) => ({
      ...work,
      ...body,
    }));
    mount(<Settings editing={null} />);
    const open = within(await screen.findByRole("article"));
    const archived = open.getByRole<HTMLSelectElement>("combobox", {
      name: "When archived",
    });
    await waitFor(() =>
      expect(archived.options[0]!.textContent).toBe("Default: leave as it is"),
    );
    expect(archived.value).toBe("");
    fireEvent.change(archived, { target: { value: "read" } });
    await waitFor(() =>
      expect(putConfigReading).toHaveBeenCalledWith("ec_1", {
        read_on_archive: true,
        read_on_trash: null,
      }),
    );
    await waitFor(() => expect(archived.value).toBe("read"));
  });

  // In a line on the page, changed in a dialog and saved together.
  it("chooses no sound and quiet hours for every notification", async () => {
    const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
    mount(<Settings editing={null} />);
    await line("With sound · No quiet hours");
    fireEvent.click(changeIn("Notifications"));
    const dialog = within(
      await screen.findByRole("form", { name: "Sound and quiet hours" }),
    );
    const sound = dialog.getByRole("switch", { name: "With sound" });
    expect(sound.getAttribute("aria-checked")).toBe("true");
    fireEvent.click(sound);
    fireEvent.click(dialog.getByRole("switch", { name: "Quiet hours" }));
    dialog.getByText(`Times are in ${zone}.`);
    fireEvent.change(dialog.getByRole("combobox", { name: "Start" }), {
      target: { value: "22:00" },
    });
    expect(putPushSettings).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(putPushSettings).toHaveBeenCalledWith({
        silent: true,
        quiet: { from: "22:00", until: "07:00", zone },
      }),
    );
    await line("Silent · Quiet hours 22:00–07:00");
  });

  // Hours set on a device elsewhere stay in their zone until this one is asked to use its own.
  it("keeps quiet hours in their time zone, and offers this device's", async () => {
    const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
    getPushSettings.mockResolvedValue({
      silent: false,
      quiet: { from: "22:30", until: "06:10", zone: "Pacific/Chatham" },
    });
    mount(<Settings editing={null} />);
    await line("With sound · Quiet hours 22:30–06:10 in Pacific/Chatham");
    fireEvent.click(changeIn("Notifications"));
    const dialog = within(
      await screen.findByRole("form", { name: "Sound and quiet hours" }),
    );
    dialog.getByText(
      `Times are in Pacific/Chatham, but this device is in ${zone}.`,
    );
    const end = dialog.getByRole<HTMLSelectElement>("combobox", {
      name: "End",
    });
    expect(end.value).toBe("06:10");
    fireEvent.change(end, { target: { value: "06:30" } });
    fireEvent.click(dialog.getByRole("button", { name: `Use ${zone}` }));
    dialog.getByText(`Times are in ${zone}.`);

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(putPushSettings).toHaveBeenCalledWith({
        silent: false,
        quiet: { from: "22:30", until: "06:30", zone },
      }),
    );
    await line("With sound · Quiet hours 22:30–06:30");
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
