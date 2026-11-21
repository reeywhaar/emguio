import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@app/api/transport";
import { Seal, SealKeySettings } from "@app/islands/app/Seal";
import { mount } from "@app/test/harness";

const postSeal = vi.fn();
const postSealCarry = vi.fn();
const postSealUnseal = vi.fn();
const postSealReset = vi.fn();
const putSeal = vi.fn();

vi.mock("@app/api/actions/seal", () => ({
  postSeal: (key: string) => postSeal(key),
  postSealCarry: (key: string) => postSealCarry(key),
  postSealUnseal: (key: string) => postSealUnseal(key),
  postSealReset: (key: string) => postSealReset(key),
  putSeal: (current: string, key: string) => putSeal(current, key),
}));
vi.mock("@app/api/actions/auth", () => ({
  postAuthLogout: async () => undefined,
}));
vi.mock("@app/leave", () => ({ leaveFor: vi.fn() }));

const type = (label: string, value: string) =>
  fireEvent.change(screen.getByLabelText(label), { target: { value } });

beforeEach(() => {
  for (const f of [
    postSeal,
    postSealCarry,
    postSealUnseal,
    postSealReset,
    putSeal,
  ]) {
    f.mockReset();
    f.mockResolvedValue(undefined);
  }
});

describe("the seal page", () => {
  it("unseals with the key, and says why a wrong one will not", async () => {
    postSealUnseal.mockRejectedValueOnce(
      new ApiError(400, "invalid", "That is not your seal key."),
    );
    mount(<Seal state="sealed" />);
    screen.getByRole("heading", { name: "Unseal" });
    type("Seal key", "a wrong seal key");
    fireEvent.click(screen.getByRole("button", { name: "Unseal" }));
    await screen.findByText("That is not your seal key.");
    type("Seal key", "the right seal key");
    fireEvent.click(screen.getByRole("button", { name: "Unseal" }));
    await waitFor(() =>
      expect(postSealUnseal).toHaveBeenLastCalledWith("the right seal key"),
    );
  });

  // Forgotten, a new one is set, typed twice alike and long enough.
  it("sets a new key in place of a forgotten one", async () => {
    mount(<Seal state="sealed" />);
    fireEvent.click(
      screen.getByRole("button", { name: "Forgot your seal key?" }),
    );
    screen.getByRole("heading", { name: "Set a new seal key" });
    type("New seal key", "short");
    type("New seal key again", "short");
    fireEvent.click(screen.getByRole("button", { name: "Set the new key" }));
    await screen.findByText("A seal key is at least 12 characters.");
    type("New seal key", "a new long seal key");
    type("New seal key again", "a new long seal key");
    fireEvent.click(screen.getByRole("button", { name: "Set the new key" }));
    await waitFor(() =>
      expect(postSealReset).toHaveBeenCalledWith("a new long seal key"),
    );
    expect(postSealUnseal).not.toHaveBeenCalled();
  });

  // A user from before seal keys keeps their passwords with the key emguio ran with.
  it("carries the passwords over with the current key", async () => {
    mount(<Seal state="none" />);
    fireEvent.click(
      screen.getByRole("button", { name: "I have the current key" }),
    );
    type("Current key", "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=");
    fireEvent.click(screen.getByRole("button", { name: "Keep my passwords" }));
    await waitFor(() =>
      expect(postSealCarry).toHaveBeenCalledWith(
        "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=",
      ),
    );
    expect(postSeal).not.toHaveBeenCalled();
  });

  it("creates a first key for a user from before seal keys", async () => {
    mount(<Seal state="none" />);
    fireEvent.click(screen.getByRole("button", { name: "Create a seal key" }));
    screen.getByRole("heading", { name: "Create a seal key" });
    type("New seal key", "a first long seal key");
    type("New seal key again", "a first long seal key");
    fireEvent.click(screen.getByRole("button", { name: "Make it" }));
    await waitFor(() =>
      expect(postSeal).toHaveBeenCalledWith("a first long seal key"),
    );
  });
});

describe("a generated seal key", () => {
  // Both fields filled and shown, to copy and keep; what is sent is what was shown.
  it("fills both fields with one to keep", async () => {
    mount(<Seal state="none" />);
    fireEvent.click(screen.getByRole("button", { name: "Create a seal key" }));
    fireEvent.click(screen.getByRole("button", { name: "Generate one" }));
    const key = screen.getByLabelText<HTMLInputElement>("New seal key");
    const again = screen.getByLabelText<HTMLInputElement>("New seal key again");
    expect(key.value).toMatch(
      /^([0-9a-hjkmnp-tv-z]{4}-){4}[0-9a-hjkmnp-tv-z]{4}$/,
    );
    expect(again.value).toBe(key.value);
    expect(key.type).toBe("text");
    screen.getByRole("button", { name: "Copy" });
    fireEvent.click(screen.getByRole("button", { name: "Make it" }));
    await waitFor(() => expect(postSeal).toHaveBeenCalledWith(key.value));
  });
});

describe("the seal key in Settings", () => {
  it("changes it with the current one", async () => {
    mount(<SealKeySettings />);
    fireEvent.click(screen.getByRole("button", { name: "Change" }));
    const form = within(
      await screen.findByRole("form", { name: "Change seal key" }),
    );
    fireEvent.change(form.getByLabelText("Current seal key"), {
      target: { value: "the current seal key" },
    });
    fireEvent.change(form.getByLabelText("New seal key"), {
      target: { value: "the next seal key" },
    });
    fireEvent.change(form.getByLabelText("New seal key again"), {
      target: { value: "the next seal key" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(putSeal).toHaveBeenCalledWith(
        "the current seal key",
        "the next seal key",
      ),
    );
  });
});
