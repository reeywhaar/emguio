import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@app/api/transport";
import type { Mailbox } from "@app/api/types";
import { FolderMenu } from "@app/islands/app/FolderMenu";
import { mount } from "@app/test/harness";

const deleteMailbox = vi.fn();

vi.mock("@app/api/actions/emailConfigs", () => ({
  getEmailConfigs: async () => [],
  deleteEmailConfigsByIdMailboxesByMailbox: (
    id: string,
    mailbox: string,
    withMail?: { read: boolean },
  ) => deleteMailbox(id, mailbox, withMail),
  postEmailConfigsByIdMailboxesByMailboxDeleteOld: vi.fn(),
  postEmailConfigsByIdMailboxesByMailboxRead: vi.fn(),
}));
vi.mock("@app/api/actions/reading", () => ({
  getReading: async () => ({ read_on_archive: false, read_on_trash: true }),
}));

const box = (
  id: string,
  path: string[],
  messages: number,
  special_use: Mailbox["special_use"] = "",
): Mailbox => ({
  id,
  name: path.join("/"),
  path,
  special_use,
  selectable: true,
  messages,
  unseen: 0,
  uid_next: 1,
  notify: false,
});
const old = box("mb_old", ["Old"], 0);
const boxes = [old, box("mb_trash", ["Trash"], 0, "trash")];

afterEach(() => vi.clearAllMocks());

describe("deleting a folder", () => {
  // Counted empty, it held mail after all: asked again with what the server says, then deleted
  // with it, the mail to Trash.
  it("asks again when the server finds mail in it", async () => {
    deleteMailbox
      .mockRejectedValueOnce(
        new ApiError(409, "not_empty", "Old holds 3 messages."),
      )
      .mockResolvedValueOnce(undefined);
    mount(<FolderMenu config="ec_1" boxes={boxes} folder={old} />);
    fireEvent.click(screen.getByRole("button", { name: "Folder:" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Delete…" }));
    await screen.findByRole("heading", { name: "Delete Old?" });
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() =>
      expect(deleteMailbox).toHaveBeenCalledWith("ec_1", "mb_old", undefined),
    );

    await screen.findByRole("heading", { name: "Delete the mail in Old too?" });
    screen.getByText(/Old holds 3 messages\. The mail goes to Trash/);
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() =>
      expect(deleteMailbox).toHaveBeenLastCalledWith("ec_1", "mb_old", {
        read: true,
      }),
    );
  });
});
