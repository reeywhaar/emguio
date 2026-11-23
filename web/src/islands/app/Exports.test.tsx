import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Export, Mailbox } from "@app/api/types";
import { Exports, useExportMail } from "@app/islands/app/Exports";
import { mount } from "@app/test/harness";

const postExport = vi.fn();
const getExports = vi.fn();
const postDelete = vi.fn();
const dismiss = vi.fn();
const download = vi.fn();
const boxes = vi.fn();

vi.mock("@app/api/actions/exports", () => ({
  postEmailConfigsByIdMailboxesByMailboxExport: (
    id: string,
    mailbox: string,
    body: unknown,
  ) => postExport(id, mailbox, body),
  getExports: () => getExports(),
  postExportsByIdDelete: (id: string, body: unknown) => postDelete(id, body),
  deleteExportsById: (id: string) => dismiss(id),
}));
vi.mock("@app/download", () => ({
  download: (url: string, name: string) => download(url, name),
}));
vi.mock("@app/api/actions/emailConfigs", () => ({
  getEmailConfigs: async () => [],
  getEmailConfigsByIdMailboxes: () => boxes(),
}));
vi.mock("@app/api/actions/reading", () => ({
  getReading: async () => ({ read_on_archive: false, read_on_trash: true }),
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
  messages: 2,
  unseen: 0,
  uid_next: 3,
  notify: false,
});
const x = (extra: Partial<Export>): Export => ({
  id: "ex_1",
  email_config: "ec_1",
  mailbox: "mb_archive",
  state: "exporting",
  total: 4,
  exported: 1,
  error: "",
  file: "Archive.mbox",
  url: "/api/exports/ex_1/file",
  ...extra,
});

afterEach(() => vi.clearAllMocks());

function Start() {
  const exportMail = useExportMail("ec_1");
  return (
    <button
      type="button"
      onClick={() =>
        exportMail.mutate({
          mailbox: "mb_archive",
          format: "mbox",
          messages: [],
          subfolders: true,
        })
      }
    >
      Go
    </button>
  );
}

describe("exports", () => {
  // Asked, it downloads at once, and its card shows how far it has got.
  it("starts a download as it is asked", async () => {
    postExport.mockResolvedValue(x({ state: "ready", exported: 0 }));
    getExports.mockResolvedValue([x({})]);
    boxes.mockResolvedValue([box("mb_archive", "Archive")]);
    mount(
      <>
        <Start />
        <Exports />
      </>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Go" }));
    await waitFor(() =>
      expect(download).toHaveBeenCalledWith(
        "/api/exports/ex_1/file",
        "Archive.mbox",
      ),
    );
    expect(postExport).toHaveBeenCalledWith("ec_1", "mb_archive", {
      format: "mbox",
      messages: [],
      subfolders: true,
    });
    await screen.findByText("Downloading Archive.mbox");
    screen.getByText("1 of 4");
  });

  // Done: what went into it goes to Trash at the press, or the card is let go.
  it("deletes what was downloaded, or is cancelled", async () => {
    getExports.mockResolvedValue([x({ state: "done", exported: 4 })]);
    boxes.mockResolvedValue([
      box("mb_archive", "Archive"),
      box("mb_trash", "Trash", "trash"),
    ]);
    postDelete.mockResolvedValue({ trash: 4, deleted: 0, folders: 1, kept: 0 });
    dismiss.mockResolvedValue(undefined);
    mount(<Exports />);
    await screen.findByText("Archive.mbox downloaded.");
    const del = screen.getByRole("button", { name: "Delete downloaded" });
    await waitFor(() => expect(del).not.toHaveProperty("disabled", true));
    fireEvent.click(del);
    await waitFor(() =>
      expect(postDelete).toHaveBeenCalledWith("ex_1", { read: true }),
    );
    await waitFor(() => expect(dismiss).toHaveBeenCalledWith("ex_1"));
  });

  // From Trash it is for good, and asks first.
  it("asks before deleting for good", async () => {
    getExports.mockResolvedValue([
      x({
        state: "done",
        exported: 2,
        mailbox: "mb_trash",
        file: "Trash.mbox",
      }),
    ]);
    boxes.mockResolvedValue([box("mb_trash", "Trash", "trash")]);
    postDelete.mockResolvedValue({ trash: 0, deleted: 2, folders: 0, kept: 0 });
    mount(<Exports />);
    await screen.findByText("Trash.mbox downloaded.");
    const del = screen.getByRole("button", { name: "Delete downloaded" });
    await waitFor(() => expect(del).not.toHaveProperty("disabled", true));
    fireEvent.click(del);
    await screen.findByRole("heading", {
      name: "Delete the 2 messages downloaded?",
    });
    expect(postDelete).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() =>
      expect(postDelete).toHaveBeenCalledWith("ex_1", { read: false }),
    );
  });

  it("is let go with Cancel", async () => {
    getExports.mockResolvedValue([x({ state: "done", exported: 4 })]);
    boxes.mockResolvedValue([box("mb_archive", "Archive")]);
    dismiss.mockResolvedValue(undefined);
    mount(<Exports />);
    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(dismiss).toHaveBeenCalledWith("ex_1"));
    expect(postDelete).not.toHaveBeenCalled();
  });
});
