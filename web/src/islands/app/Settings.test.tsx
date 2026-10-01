import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { EmailConfig } from "@app/api/types";
import { Settings } from "@app/islands/app/Settings";
import { mount } from "@app/test/harness";

const getEmailConfigs = vi.fn();
const deleteEmailConfigsById = vi.fn();
const postEmailConfigsByIdTest = vi.fn();

vi.mock("@app/api/actions/auth", () => ({
  getAuthMe: async () => ({ id: "u_1", username: "misha", created_at: 0 }),
  postAuthLogout: async () => undefined,
}));
vi.mock("@app/leave", () => ({ leaveFor: vi.fn() }));
vi.mock("@app/api/actions/emailConfigs", () => ({
  getEmailConfigs: () => getEmailConfigs(),
  deleteEmailConfigsById: (id: string) => deleteEmailConfigsById(id),
  postEmailConfigsByIdTest: (id: string, body: unknown) =>
    postEmailConfigsByIdTest(id, body),
}));

const work: EmailConfig = {
  id: "ec_1",
  name: "Work",
  email: "misha@example.com",
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
};

beforeEach(() => {
  getEmailConfigs.mockResolvedValue([work]);
});
afterEach(() => vi.clearAllMocks());

describe("settings", () => {
  it("lists each email config with its servers", async () => {
    mount(<Settings />);
    await screen.findByText("Work");
    screen.getByText("misha@example.com");
    screen.getByText("IMAP · imap.example.com:993 · TLS");
    screen.getByText("None");
  });

  // On a phone the header has no room for it, so this is where signing out is.
  it("says who is signed in and offers a way out", async () => {
    mount(<Settings />);
    await screen.findByText("misha");
    screen.getByRole("button", { name: "Sign out" });
  });

  it("says there are none rather than showing an empty list", async () => {
    getEmailConfigs.mockResolvedValue([]);
    mount(<Settings />);
    await screen.findByText(/None yet/);
  });

  // A button that stops to ask ends in an ellipsis, and the asking names what goes.
  it("asks before deleting, and names what it deletes", async () => {
    deleteEmailConfigsById.mockResolvedValue(undefined);
    mount(<Settings />);
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
    mount(<Settings />);
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
});
