import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Mailbox, Transfer } from "@app/api/types";
import { Transfers } from "@app/islands/app/Transfers";
import { mount } from "@app/test/harness";

const getTransfers = vi.fn();
const cancel = vi.fn();
const dismiss = vi.fn();
const retry = vi.fn();

vi.mock("@app/api/actions/transfers", () => ({
  getTransfers: () => getTransfers(),
  postTransfersByIdCancel: (id: string) => cancel(id),
  deleteTransfersById: (id: string) => dismiss(id),
  postTransfersByIdRetry: (id: string) => retry(id),
}));

const box = (
  id: string,
  name: string,
  special_use: Mailbox["special_use"] = "",
): Mailbox => ({
  id,
  name,
  path: [name],
  special_use,
  selectable: true,
  messages: 0,
  unseen: 0,
  uid_next: 1,
  notify: false,
});

vi.mock("@app/api/actions/emailConfigs", () => ({
  getEmailConfigs: async () => [
    { id: "ec_1", name: "Work", email: "misha@example.com" },
    { id: "ec_2", name: "Personal", email: "misha@example.org" },
  ],
  getEmailConfigsByIdMailboxes: async (id: string) =>
    id === "ec_2"
      ? [box("mb_archive", "Archive", "archive")]
      : [box("mb_inbox", "INBOX", "inbox")],
}));

const transfer = (extra: Partial<Transfer> = {}): Transfer => ({
  id: "tr_1",
  email_config: "ec_1",
  mailbox: "mb_inbox",
  target_config: "ec_2",
  target: "mb_archive",
  total: 1000,
  moved: 340,
  state: "moving",
  error: "",
  created_at: Math.floor(Date.now() / 1000),
  ...extra,
});

beforeEach(() => {
  dismiss.mockResolvedValue(undefined);
});
afterEach(() => vi.clearAllMocks());

describe("transfers", () => {
  // How far it has got, and cancelling what is left: what moved stays moved.
  it("shows a move to another account and cancels what is left", async () => {
    getTransfers.mockResolvedValue([transfer()]);
    cancel.mockResolvedValue(transfer({ state: "cancelled" }));
    mount(<Transfers />);
    await screen.findByText("Moving to Personal › Archive");
    screen.getByText("340 of 1000");

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(cancel).toHaveBeenCalledWith("tr_1"));
    await screen.findByText("Cancelled: 340 moved, 660 still in Inbox.");
  });

  // A server's refusal is said, and stays until dismissed.
  it("says why a move stopped, until dismissed", async () => {
    getTransfers.mockResolvedValue([
      transfer({ state: "stopped", error: "Over quota." }),
    ]);
    mount(<Transfers />);
    await screen.findByText(/Over quota\./);
    await screen.findByText(/340 moved, 660 still in Inbox\./);
    expect(dismiss).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    await waitFor(() => expect(dismiss).toHaveBeenCalledWith("tr_1"));
  });

  // Tried again, what it left goes on moving, counted from what had moved.
  it("tries a stopped move again", async () => {
    getTransfers.mockResolvedValue([
      transfer({ state: "stopped", error: "Over quota." }),
    ]);
    retry.mockResolvedValue(transfer());
    mount(<Transfers />);
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    await waitFor(() => expect(retry).toHaveBeenCalledWith("tr_1"));
    await screen.findByText("Moving to Personal › Archive");
    screen.getByText("340 of 1000");
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
  });

  // A few messages moved at once need no card: the list shows they went.
  it("lets a small move that finished go without a card", async () => {
    getTransfers.mockResolvedValue([
      transfer({ total: 2, moved: 2, state: "done" }),
    ]);
    mount(<Transfers />);
    await waitFor(() => expect(dismiss).toHaveBeenCalledWith("tr_1"));
    expect(screen.queryByRole("status")).toBeNull();
  });
});
