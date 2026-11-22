import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Session } from "@app/api/types";
import { Sessions } from "@app/islands/app/Sessions";
import { mount } from "@app/test/harness";

const getSessions = vi.fn();
const deleteSessionsById = vi.fn();
const deleteSessions = vi.fn();
const postAuthLogout = vi.fn();
const leaveFor = vi.fn();

vi.mock("@app/api/actions/sessions", () => ({
  getSessions: () => getSessions(),
  deleteSessionsById: (id: string) => deleteSessionsById(id),
  deleteSessions: () => deleteSessions(),
}));
vi.mock("@app/api/actions/auth", () => ({
  postAuthLogout: () => postAuthLogout(),
}));
vi.mock("@app/leave", () => ({ leaveFor: (path: string) => leaveFor(path) }));
vi.mock("@app/push", () => ({ forget: async () => undefined }));

const session = (extra: Partial<Session>): Session => ({
  id: "s_000000000001",
  current: false,
  created_at: 1790000000,
  last_seen_at: 1790000000,
  expires_at: 1790600000,
  device: "Firefox on a Mac",
  user_agent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Firefox/140.0",
  ...extra,
});

afterEach(() => vi.clearAllMocks());

describe("sessions", () => {
  // Another browser signs out at once; this one asks first, and goes to the sign-in page.
  it("signs out a session, and this browser after asking", async () => {
    getSessions.mockResolvedValue([
      session({ id: "s_000000000001", current: true }),
      session({ id: "s_000000000002", device: "Safari on iPhone" }),
    ]);
    deleteSessionsById.mockResolvedValue(undefined);
    postAuthLogout.mockResolvedValue(undefined);
    mount(<Sessions />);
    const list = await screen.findByRole("list", { name: "Sessions" });
    const [here, there] = within(list).getAllByRole("listitem") as [
      HTMLElement,
      HTMLElement,
    ];
    within(here).getByText(/this browser/);
    within(there).getByText(/last used/);

    fireEvent.click(within(there).getByRole("button", { name: "Sign out" }));
    await waitFor(() =>
      expect(deleteSessionsById).toHaveBeenCalledWith("s_000000000002"),
    );

    fireEvent.click(within(here).getByRole("button", { name: "Sign out…" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Sign out" }));
    await waitFor(() => expect(leaveFor).toHaveBeenCalledWith("/login"));
    expect(postAuthLogout).toHaveBeenCalled();
    expect(deleteSessionsById).toHaveBeenCalledTimes(1);
  });

  it("signs out everywhere else, offered only when there is somewhere else", async () => {
    getSessions.mockResolvedValue([
      session({ current: true }),
      session({ id: "s_000000000002" }),
    ]);
    deleteSessions.mockResolvedValue(undefined);
    mount(<Sessions />);
    const button = await screen.findByRole("button", {
      name: "Sign out everywhere else",
    });
    getSessions.mockResolvedValue([session({ current: true })]);
    fireEvent.click(button);
    await waitFor(() => expect(deleteSessions).toHaveBeenCalled());
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "Sign out everywhere else" }),
      ).toBeNull(),
    );
  });
});
