import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@app/api/transport";
import type { EmailConfig } from "@app/api/types";
import { ConfigForm } from "@app/islands/app/ConfigForm";
import { mount } from "@app/test/harness";

const getEmailConfigs = vi.fn();
const postEmailConfigs = vi.fn();
const putEmailConfigsById = vi.fn();
const postEmailConfigsTest = vi.fn();
const postEmailConfigsByIdTest = vi.fn();
const onClose = vi.fn();

vi.mock("@app/api/actions/emailConfigs", () => ({
  getEmailConfigs: () => getEmailConfigs(),
  postEmailConfigs: (body: unknown) => postEmailConfigs(body),
  putEmailConfigsById: (id: string, body: unknown) =>
    putEmailConfigsById(id, body),
  postEmailConfigsTest: (body: unknown) => postEmailConfigsTest(body),
  postEmailConfigsByIdTest: (id: string, body: unknown) =>
    postEmailConfigsByIdTest(id, body),
}));

const saved: EmailConfig = {
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
  outgoing: {
    host: "smtp.example.com",
    port: 465,
    tls: "implicit",
    username: "",
  },
  created_at: 0,
  updated_at: 0,
  synced_at: null,
  sync_error: "",
  inbox_unseen: 0,
};

beforeEach(() => {
  getEmailConfigs.mockResolvedValue([saved]);
  postEmailConfigs.mockResolvedValue(saved);
  putEmailConfigsById.mockResolvedValue(saved);
});

afterEach(() => vi.clearAllMocks());

const field = (label: string) =>
  screen.getByLabelText<HTMLInputElement>(label, { selector: "input,select" });

const type = (label: string, value: string) =>
  fireEvent.change(field(label), { target: { value } });

describe("adding an email config", () => {
  it("sends the whole draft and closes", async () => {
    mount(<ConfigForm open id={null} onClose={onClose} />);
    type("Email address", "misha@example.com");
    type("Host", "imap.example.com");
    type("Password", "hunter2");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(postEmailConfigs).toHaveBeenCalledWith({
      name: "",
      email: "misha@example.com",
      sender_name: "",
      incoming: {
        protocol: "imap",
        host: "imap.example.com",
        port: 993,
        tls: "implicit",
        // The address, because nobody typed a username of their own.
        username: "misha@example.com",
        password: "hunter2",
      },
      outgoing: null,
    });
  });

  it("stops the username following the address once it is typed", () => {
    mount(<ConfigForm open id={null} onClose={onClose} />);
    type("Email address", "misha@example.com");
    type("Username", "misha");
    type("Email address", "robin@example.com");
    expect(field("Username").value).toBe("misha");
  });

  // A port still at the usual one is the usual one for the setting, not a choice somebody made.
  it("moves a usual port with the security setting and leaves a typed one", () => {
    mount(<ConfigForm open id={null} onClose={onClose} />);
    type("Security", "starttls");
    expect(field("Port").value).toBe("143");

    type("Port", "1143");
    type("Security", "implicit");
    expect(field("Port").value).toBe("1143");
  });

  it("sends an outgoing server that shares the sign-in with no username of its own", async () => {
    mount(<ConfigForm open id={null} onClose={onClose} />);
    type("Email address", "misha@example.com");
    type("Password", "hunter2");
    fireEvent.click(screen.getByLabelText("Send mail through an SMTP server"));
    const hosts = screen.getAllByLabelText<HTMLInputElement>("Host");
    fireEvent.change(hosts[1]!, { target: { value: "smtp.example.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(postEmailConfigs).toHaveBeenCalled());
    expect(postEmailConfigs.mock.calls[0]![0].outgoing).toEqual({
      host: "smtp.example.com",
      port: 465,
      tls: "implicit",
      username: "",
      password: "",
    });
  });

  // The server writes the sentence for whoever reads it.
  it("says what the server refused", async () => {
    postEmailConfigs.mockRejectedValue(
      new ApiError(400, "invalid", "The incoming server needs a host."),
    );
    mount(<ConfigForm open id={null} onClose={onClose} />);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await screen.findByText("The incoming server needs a host.");
    expect(onClose).not.toHaveBeenCalled();
  });

  it("is a dialog that can be left without saving", () => {
    mount(<ConfigForm open id={null} onClose={onClose} />);
    screen.getByRole("dialog");
    screen.getByRole("heading", { name: "Add mail account" });
    type("Email address", "misha@example.com");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(2);
    expect(postEmailConfigs).not.toHaveBeenCalled();
  });

  it("starts in the address field", () => {
    mount(<ConfigForm open id={null} onClose={onClose} />);
    expect(document.activeElement).toBe(field("Email address"));
  });

  it("shows what each server said to a test", async () => {
    postEmailConfigsTest.mockResolvedValue({
      incoming: { ok: true, message: "" },
      outgoing: {
        ok: false,
        message: "smtp.example.com:465 refused the username or password.",
      },
    });
    mount(<ConfigForm open id={null} onClose={onClose} />);
    fireEvent.click(screen.getByRole("button", { name: "Test connection" }));
    await screen.findByText("Incoming: signed in.");
    screen.getByText(
      "Outgoing: smtp.example.com:465 refused the username or password.",
    );
  });
});

describe("editing an email config", () => {
  // The form never has the saved password, so empty is how it says "the one I saved".
  it("leaves the saved passwords out", async () => {
    mount(<ConfigForm open id="ec_1" onClose={onClose} />);
    await screen.findByDisplayValue("imap.example.com");
    screen.getByRole("heading", { name: "Edit Work" });
    type("Name", "Personal");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(putEmailConfigsById).toHaveBeenCalled());
    const [id, body] = putEmailConfigsById.mock.calls[0]!;
    expect(id).toBe("ec_1");
    expect(body.name).toBe("Personal");
    expect(body.incoming.password).toBe("");
    expect(body.outgoing).toEqual({
      host: "smtp.example.com",
      port: 465,
      tls: "implicit",
      username: "",
      password: "",
    });
  });

  it("tests with the saved config's id, so its saved passwords are used", async () => {
    postEmailConfigsByIdTest.mockResolvedValue({
      incoming: { ok: true, message: "" },
      outgoing: null,
    });
    mount(<ConfigForm open id="ec_1" onClose={onClose} />);
    await screen.findByDisplayValue("imap.example.com");
    fireEvent.click(screen.getByRole("button", { name: "Test connection" }));
    await screen.findByText("Incoming: signed in.");
    expect(postEmailConfigsByIdTest.mock.calls[0]![0]).toBe("ec_1");
    expect(postEmailConfigsTest).not.toHaveBeenCalled();
  });

  it("says so when there is no such config", async () => {
    mount(<ConfigForm open id="ec_gone" onClose={onClose} />);
    await screen.findByText("There is no such mail account.");
  });
});
