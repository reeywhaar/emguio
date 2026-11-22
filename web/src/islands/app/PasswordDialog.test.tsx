import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@app/api/transport";
import { PasswordDialog } from "@app/islands/app/PasswordDialog";
import { mount } from "@app/test/harness";

const putAuthPassword = vi.fn();

vi.mock("@app/api/actions/auth", () => ({
  putAuthPassword: (body: unknown) => putAuthPassword(body),
}));

afterEach(() => vi.clearAllMocks());

const type = (label: string, value: string) =>
  fireEvent.change(screen.getByLabelText(label), { target: { value } });

describe("changing the password", () => {
  // Two new passwords that differ are caught here: the server is sent one.
  it("says what is wrong before asking the server", async () => {
    mount(<PasswordDialog open onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await screen.findByText("Enter your current password.");
    type("Current password", "a good password");
    type("New password", "a new password");
    type("New password again", "another one");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await screen.findByText("The two new passwords do not match.");
    expect(putAuthPassword).not.toHaveBeenCalled();
  });

  it("says the server's refusal, and closes once changed", async () => {
    const onClose = vi.fn();
    putAuthPassword.mockRejectedValueOnce(
      new ApiError(400, "invalid", "That is not your current password."),
    );
    mount(<PasswordDialog open onClose={onClose} />);
    type("Current password", "not it");
    type("New password", "a new password");
    type("New password again", "a new password");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await screen.findByText("That is not your current password.");
    expect(onClose).not.toHaveBeenCalled();

    putAuthPassword.mockResolvedValueOnce(undefined);
    type("Current password", "a good password");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(putAuthPassword).toHaveBeenLastCalledWith({
      current: "a good password",
      password: "a new password",
    });
  });
});
