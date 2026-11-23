import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Import, Mailbox } from "@app/api/types";
import { ImportDialog } from "@app/islands/app/ImportDialog";
import { Imports } from "@app/islands/app/Imports";
import { mount } from "@app/test/harness";

const upload = vi.fn();
const getImports = vi.fn();
const cancel = vi.fn();
const dismiss = vi.fn();

vi.mock("@app/api/actions/imports", async (actual) => ({
  ...(await actual<typeof import("@app/api/actions/imports")>()),
  postEmailConfigsByIdMailboxesByMailboxImport: (
    id: string,
    mailbox: string,
    file: File,
    kind: string,
    onProgress: (sent: number, total: number) => void,
  ) => upload(id, mailbox, file, kind, onProgress),
  getImports: () => getImports(),
  postImportsByIdCancel: (id: string) => cancel(id),
  deleteImportsById: (id: string) => dismiss(id),
}));
vi.mock("@app/api/actions/emailConfigs", () => ({
  getEmailConfigsByIdMailboxes: async () => [archive],
}));

const archive: Mailbox = {
  id: "mb_archive",
  name: "Archive",
  path: ["Archive"],
  special_use: "",
  selectable: true,
  messages: 0,
  unseen: 0,
  uid_next: 1,
  notify: false,
};
const started = (extra: Partial<Import> = {}): Import => ({
  id: "im_1",
  email_config: "ec_1",
  mailbox: "mb_archive",
  file: "Old.mbox",
  state: "importing",
  total: 10,
  imported: 3,
  skipped: 1,
  folders: 0,
  error: "",
  created_at: 0,
  ...extra,
});

afterEach(() => vi.clearAllMocks());

const choose = (name: string) =>
  fireEvent.change(screen.getByLabelText("File to import"), {
    target: { files: [new File(["From x"], name)] },
  });

describe("importing", () => {
  // The file goes as itself, by its kind; the dialog closes as the import begins.
  it("uploads a file and closes", async () => {
    const onClose = vi.fn();
    upload.mockResolvedValue(started());
    mount(
      <ImportDialog config="ec_1" folder={archive} open onClose={onClose} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Import" }));
    await screen.findByText("Choose a file to import.");
    choose("notes.txt");
    fireEvent.click(screen.getByRole("button", { name: "Import" }));
    await screen.findByText("Import an .mbox, an .eml, or a .zip of them.");
    expect(upload).not.toHaveBeenCalled();

    choose("Old.mbox");
    fireEvent.click(screen.getByRole("button", { name: "Import" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    const [id, mailbox, file, kind] = upload.mock.calls[0] as [
      string,
      string,
      File,
      string,
    ];
    expect([id, mailbox, file.name, kind]).toEqual([
      "ec_1",
      "mb_archive",
      "Old.mbox",
      "application/mbox",
    ]);
  });

  // In the corner while it runs, cancelled there; a failure stays until dismissed.
  it("shows an import in the corner", async () => {
    getImports.mockResolvedValue([started()]);
    cancel.mockResolvedValue(started({ state: "cancelled" }));
    mount(<Imports />);
    await screen.findByText("Importing Old.mbox into Archive");
    screen.getByText("4 of 10");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(cancel).toHaveBeenCalledWith("im_1"));
  });

  it("says why an import failed, until dismissed", async () => {
    getImports.mockResolvedValue([
      started({ state: "failed", error: "Over quota." }),
    ]);
    dismiss.mockResolvedValue(undefined);
    mount(<Imports />);
    await screen.findByText(/Over quota\./);
    expect(dismiss).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    await waitFor(() => expect(dismiss).toHaveBeenCalledWith("im_1"));
  });
});
