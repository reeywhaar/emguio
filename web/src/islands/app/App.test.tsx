import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { App } from "@app/islands/app/App";
import { mount } from "@app/test/harness";

const getAuthMe = vi.fn();
const postAuthLogout = vi.fn();
const leaveFor = vi.fn();

vi.mock("@app/api/actions/auth", () => ({
  getAuthMe: () => getAuthMe(),
  postAuthLogout: () => postAuthLogout(),
}));
vi.mock("@app/leave", () => ({ leaveFor: (path: string) => leaveFor(path) }));

afterEach(() => vi.clearAllMocks());

describe("the application", () => {
  it("says who is signed in", async () => {
    getAuthMe.mockResolvedValue({
      id: "u_1",
      username: "misha",
      created_at: 0,
    });
    mount(<App />);
    await screen.findByText("misha");
  });

  it("signs out and goes to the sign-in page", async () => {
    getAuthMe.mockResolvedValue({
      id: "u_1",
      username: "misha",
      created_at: 0,
    });
    postAuthLogout.mockResolvedValue(undefined);
    mount(<App />);

    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));

    await waitFor(() => expect(leaveFor).toHaveBeenCalledWith("/login"));
    expect(postAuthLogout).toHaveBeenCalled();
  });
});
