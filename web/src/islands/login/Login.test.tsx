import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@app/api/transport";
import { Login } from "@app/islands/login/Login";

const postAuthLogin = vi.fn();
const getAuthInvitesByToken = vi.fn();
const postAuthInvitesByTokenAccept = vi.fn();
const leaveFor = vi.fn();

vi.mock("@app/api/actions/auth", () => ({
  postAuthLogin: (body: unknown) => postAuthLogin(body),
  getAuthInvitesByToken: (token: string) => getAuthInvitesByToken(token),
  postAuthInvitesByTokenAccept: (token: string, body: unknown) =>
    postAuthInvitesByTokenAccept(token, body),
}));
vi.mock("@app/leave", () => ({ leaveFor: (path: string) => leaveFor(path) }));

afterEach(() => {
  window.history.pushState({}, "", "/login");
  vi.clearAllMocks();
});

const type = (placeholder: string, value: string) =>
  fireEvent.change(screen.getByPlaceholderText(placeholder), {
    target: { value },
  });

describe("signing in", () => {
  it("goes to the application once the password matches", async () => {
    postAuthLogin.mockResolvedValue(undefined);
    render(<Login />);

    type("Username", "misha");
    type("Password", "a good password");
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    await waitFor(() => expect(leaveFor).toHaveBeenCalledWith("/"));
    expect(postAuthLogin).toHaveBeenCalledWith({
      username: "misha",
      password: "a good password",
    });
  });

  // The server writes the sentence for whoever reads it.
  it("says what the server said when it does not", async () => {
    postAuthLogin.mockRejectedValue(
      new ApiError(
        401,
        "unauthenticated",
        "That username and password do not match.",
      ),
    );
    render(<Login />);

    type("Username", "misha");
    type("Password", "not it");
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    await screen.findByText("That username and password do not match.");
    expect(leaveFor).not.toHaveBeenCalled();
  });
});

/** An invitation is a path, so the page is opened by putting one in the address bar. */
const open = (token = "a-token") => {
  window.history.pushState({}, "", `/invite/${token}`);
  return render(<Login />);
};

describe("an invitation link", () => {
  it("makes a user and signs them in", async () => {
    getAuthInvitesByToken.mockResolvedValue({ expires_at: 0 });
    postAuthInvitesByTokenAccept.mockResolvedValue(undefined);
    open();

    type("Username", "misha");
    type("Password", "a good password");
    const sealKey = screen.getByLabelText("Seal key");
    const again = screen.getByLabelText("Seal key again");
    fireEvent.change(sealKey, { target: { value: "a long seal key" } });
    fireEvent.change(again, { target: { value: "a long seal kez" } });
    const create = screen.getByRole<HTMLButtonElement>("button", {
      name: "Create it",
    });
    await waitFor(() => expect(create.disabled).toBe(false));
    // Typed twice, alike, before anything is sent.
    fireEvent.click(create);
    await screen.findByText("The two seal keys do not match.");
    expect(postAuthInvitesByTokenAccept).not.toHaveBeenCalled();

    fireEvent.change(again, { target: { value: "a long seal key" } });
    fireEvent.click(create);
    await waitFor(() => expect(leaveFor).toHaveBeenCalledWith("/"));
    expect(postAuthInvitesByTokenAccept).toHaveBeenCalledWith("a-token", {
      username: "misha",
      password: "a good password",
      seal_key: "a long seal key",
    });
  });

  // Before somebody types a password into a form that cannot take it.
  it("says it is dead before anything is typed", async () => {
    getAuthInvitesByToken.mockRejectedValue(
      new ApiError(404, "not_found", "That invitation has been used."),
    );
    open();
    await screen.findByText(/used or has expired/i);
    expect(screen.queryByPlaceholderText("Password")).toBeNull();
  });
});
