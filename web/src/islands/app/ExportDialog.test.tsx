import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Export, Mailbox } from "@app/api/types";
import { ExportDialog } from "@app/islands/app/ExportDialog";
import { mount } from "@app/test/harness";

const postExport = vi.fn();
const getExport = vi.fn();
const postDelete = vi.fn();
const download = vi.fn();

vi.mock("@app/api/actions/exports", () => ({
  postEmailConfigsByIdMailboxesByMailboxExport: (
    id: string,
    mailbox: string,
    body: unknown,
  ) => postExport(id, mailbox, body),
  getExportsById: (id: string) => getExport(id),
  postExportsByIdDelete: (id: string, body: unknown) => postDelete(id, body),
}));
vi.mock("@app/download", () => ({
  download: (url: string, name: string) => download(url, name),
}));
vi.mock("@app/api/actions/emailConfigs", () => ({
  getEmailConfigs: async () => [],
}));
vi.mock("@app/api/actions/reading", () => ({
  getReading: async () => ({ read_on_archive: false, read_on_trash: true }),
}));

const box = (
  id: string,
  path: string[],
  special_use: Mailbox["special_use"] = "",
): Mailbox => ({
  id,
  name: path.join("/"),
  path,
  special_use,
  selectable: true,
  messages: 2,
  unseen: 0,
  uid_next: 3,
  notify: false,
});
const archive = box("mb_archive", ["Archive"]);
const boxes = [
  archive,
  box("mb_2025", ["Archive", "2025"]),
  box("mb_trash", ["Trash"], "trash"),
];
const x = (extra: Partial<Export>): Export => ({
  id: "ex_1",
  state: "ready",
  total: 4,
  exported: 0,
  error: "",
  file: "Archive.zip",
  url: "/api/exports/ex_1/file",
  ...extra,
});

afterEach(() => vi.clearAllMocks());

describe("exporting", () => {
  // The format and the subfolders, then the download, then deleting what went into it.
  it("exports a folder tree and deletes what was exported", async () => {
    const onDeleted = vi.fn();
    postExport.mockResolvedValue(x({}));
    getExport.mockResolvedValue(x({ state: "done", exported: 4 }));
    postDelete.mockResolvedValue({ trash: 4, deleted: 0 });
    mount(
      <ExportDialog
        config="ec_1"
        folder={archive}
        boxes={boxes}
        open
        onClose={vi.fn()}
        onDeleted={onDeleted}
      />,
    );
    fireEvent.change(screen.getByLabelText("Format"), {
      target: { value: "eml" },
    });
    fireEvent.click(
      screen.getByRole("switch", { name: "With its subfolders" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Export" }));
    await waitFor(() =>
      expect(postExport).toHaveBeenCalledWith("ec_1", "mb_archive", {
        format: "eml",
        messages: [],
        subfolders: true,
      }),
    );
    expect(download).toHaveBeenCalledWith(
      "/api/exports/ex_1/file",
      "Archive.zip",
    );

    await screen.findByText("Exported 4 messages as Archive.zip.");
    fireEvent.click(screen.getByRole("button", { name: "Delete them…" }));
    await screen.findByRole("heading", {
      name: "Delete the 4 messages exported?",
    });
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() =>
      expect(postDelete).toHaveBeenCalledWith("ex_1", { read: true }),
    );
    await waitFor(() => expect(onDeleted).toHaveBeenCalled());
  });

  // A selection goes without subfolders, and a failure is said.
  it("exports a selection, and says why it stopped", async () => {
    postExport.mockResolvedValue(x({ file: "Archive.mbox" }));
    getExport.mockResolvedValue(
      x({ state: "failed", error: "The mail server stopped answering." }),
    );
    mount(
      <ExportDialog
        config="ec_1"
        folder={archive}
        boxes={boxes}
        messages={["7-1", "7-2"]}
        open
        onClose={vi.fn()}
      />,
    );
    screen.getByRole("heading", { name: "Export 2 messages" });
    expect(screen.queryByRole("switch")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Export" }));
    await waitFor(() =>
      expect(postExport).toHaveBeenCalledWith("ec_1", "mb_archive", {
        format: "mbox",
        messages: ["7-1", "7-2"],
        subfolders: false,
      }),
    );
    await screen.findByText("The mail server stopped answering.");
  });
});
