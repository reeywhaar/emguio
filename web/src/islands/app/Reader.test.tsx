import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@app/api/transport";
import type { Mailbox, ReadMessage } from "@app/api/types";
import { Reader } from "@app/islands/app/Reader";
import { mount } from "@app/test/harness";

const getMessage = vi.fn();

vi.mock("@app/api/actions/emailConfigs", () => ({
  getEmailConfigsByIdMessagesByMessage: (
    id: string,
    message: string,
    images: boolean,
  ) => getMessage(id, message, images),
  partURL: (id: string, message: string, index: number) =>
    `/parts/${id}/${message}/${index}`,
}));

const inbox: Mailbox = {
  id: "mb_inbox",
  name: "INBOX",
  path: ["INBOX"],
  special_use: "inbox",
  selectable: true,
  messages: 1,
  unseen: 0,
};

const read = (extra: Partial<ReadMessage> = {}): ReadMessage => ({
  id: "m_1",
  from: { name: "Alice", email: "alice@example.com" },
  to: [{ name: "", email: "misha@example.com" }],
  cc: [{ name: "Bob", email: "bob@example.com" }],
  subject: "Quarterly numbers",
  date: 1790000000,
  sent: null,
  seen: false,
  flagged: false,
  answered: false,
  draft: false,
  has_attachments: true,
  preview: "",
  mailbox: "mb_inbox",
  text: "The numbers are in.",
  html: "",
  remote_images: 0,
  parts: [
    {
      index: 0,
      name: "q3.pdf",
      type: "application/pdf",
      size: 12800,
      listed: true,
    },
    { index: 1, name: "logo.png", type: "image/png", size: 300, listed: false },
  ],
  ...extra,
});

beforeEach(() => {
  getMessage.mockResolvedValue(read());
});
afterEach(() => vi.clearAllMocks());

const open = () =>
  mount(<Reader config="ec_1" mailbox={inbox} message="m_1" />);

describe("the reading pane", () => {
  it("says who, to whom, and what", async () => {
    open();
    await screen.findByRole("heading", { name: "Quarterly numbers" });
    screen.getByText("Alice");
    screen.getByText("<alice@example.com>");
    screen.getByText("misha@example.com");
    screen.getByText("Bob");
    screen.getByText("The numbers are in.");
  });

  // An image the HTML already shows in place is not an attachment to anybody reading it.
  it("lists the attachments and not the images shown in place", async () => {
    open();
    const list = await screen.findByRole("list", { name: "Attachments" });
    const links = within(list).getAllByRole("link");
    expect(links).toHaveLength(1);
    expect(links[0]!.getAttribute("href")).toBe("/parts/ec_1/m_1/0");
    within(links[0]!).getByText("q3.pdf");
    within(links[0]!).getByText("13 KB");
  });

  // The frame is the second wall: no scripts, and images only from here.
  it("shows HTML in a frame that runs nothing and loads nothing from elsewhere", async () => {
    getMessage.mockResolvedValue(read({ html: "<p>Rich</p>" }));
    open();
    const frame = await screen.findByTitle<HTMLIFrameElement>("Message");
    expect(frame.getAttribute("sandbox")).not.toContain("allow-scripts");
    const doc = frame.getAttribute("srcdoc")!;
    expect(doc).toContain("<p>Rich</p>");
    expect(doc).toContain("default-src 'none'; img-src 'self' data:");
  });

  it("asks before loading images from elsewhere", async () => {
    getMessage.mockImplementation(
      async (_c: string, _m: string, images: boolean) =>
        read({ html: "<p>Rich</p>", remote_images: images ? 0 : 3 }),
    );
    open();
    await screen.findByText("3 images from the internet are not shown.");
    expect(getMessage).toHaveBeenLastCalledWith("ec_1", "m_1", false);
    fireEvent.click(screen.getByRole("button", { name: "Show images" }));
    await waitFor(() =>
      expect(getMessage).toHaveBeenLastCalledWith("ec_1", "m_1", true),
    );
    await waitFor(() =>
      expect(
        screen.queryByText(/images from the internet are not shown/),
      ).toBeNull(),
    );
  });

  it("says what the server said when the message cannot be had", async () => {
    getMessage.mockRejectedValue(
      new ApiError(404, "gone", "This message is no longer on the server."),
    );
    open();
    await screen.findByText("This message is no longer on the server.");
  });
});
