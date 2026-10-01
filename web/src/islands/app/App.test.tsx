import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { EmailConfig } from "@app/api/types";
import { App } from "@app/islands/app/App";
import { mount } from "@app/test/harness";

const getAuthMe = vi.fn();
const postAuthLogout = vi.fn();
const getEmailConfigs = vi.fn();
const leaveFor = vi.fn();

vi.mock("@app/api/actions/auth", () => ({
  getAuthMe: () => getAuthMe(),
  postAuthLogout: () => postAuthLogout(),
}));
vi.mock("@app/api/actions/emailConfigs", () => ({
  getEmailConfigs: () => getEmailConfigs(),
}));
vi.mock("@app/leave", () => ({ leaveFor: (path: string) => leaveFor(path) }));

const config = (id: string, name: string): EmailConfig => ({
  id,
  name,
  email: `${name.toLowerCase()}@example.com`,
  incoming: {
    protocol: "imap",
    host: "imap.example.com",
    port: 993,
    tls: "implicit",
    username: "misha",
  },
  outgoing: null,
  created_at: 0,
  updated_at: 0,
});

beforeEach(() => {
  window.history.pushState({}, "", "/");
  window.localStorage.clear();
  getAuthMe.mockResolvedValue({ id: "u_1", username: "misha", created_at: 0 });
  getEmailConfigs.mockResolvedValue([]);
});

afterEach(() => vi.clearAllMocks());

describe("the application", () => {
  it("says who is signed in", async () => {
    mount(<App />);
    await screen.findByText("misha");
  });

  it("signs out and goes to the sign-in page", async () => {
    postAuthLogout.mockResolvedValue(undefined);
    mount(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await waitFor(() => expect(leaveFor).toHaveBeenCalledWith("/login"));
  });

  it("points somewhere when there are no email configs yet", async () => {
    mount(<App />);
    fireEvent.click(
      await screen.findByRole("link", { name: "Add an email config" }),
    );
    expect(window.location.pathname).toBe("/settings/email-configs/new");
    await screen.findByRole("heading", { name: "Add an email config" });
  });

  // One config at a time, chosen in the header, and the address says which.
  it("switches email config from the header", async () => {
    getEmailConfigs.mockResolvedValue([
      config("ec_1", "Work"),
      config("ec_2", "Home"),
    ]);
    mount(<App />);
    await screen.findByRole("heading", { name: "Work" });

    fireEvent.change(screen.getByLabelText("Email config"), {
      target: { value: "ec_2" },
    });
    expect(window.location.pathname).toBe("/c/ec_2");
    await screen.findByRole("heading", { name: "Home" });
  });

  it("opens on the config last looked at", async () => {
    getEmailConfigs.mockResolvedValue([
      config("ec_1", "Work"),
      config("ec_2", "Home"),
    ]);
    window.localStorage.setItem("emguio.email-config", "ec_2");
    mount(<App />);
    await screen.findByRole("heading", { name: "Home" });
  });
});
