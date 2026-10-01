import { MutationObserver } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { mk } from "@app/api/keys";
import type { Job } from "@app/api/types";
import { Jobs } from "@app/islands/app/Jobs";
import { mount } from "@app/test/harness";

const getJobs = vi.fn();

vi.mock("@app/api/actions/jobs", () => ({
  getJobs: () => getJobs(),
  deleteJobsById: async () => undefined,
}));

const waiting = (id: string): Job => ({
  id,
  email_config: "ec_1",
  mailbox: "mb_inbox",
  message: "1-1",
  kind: "seen",
  value: true,
  target: "",
  seen: false,
  error: "",
  created_at: 0,
});

afterEach(() => vi.clearAllMocks());

/** Whether closing the tab now would be stopped to ask. */
const leave = () => {
  const e = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(e);
  return e.defaultPrevented;
};

describe("what is still being done", () => {
  // A page opened after the jobs were asked shows them too: they are the server's.
  it("is shown in the corner while the server has jobs waiting", async () => {
    getJobs.mockResolvedValue([waiting("j_1"), waiting("j_2")]);
    mount(<Jobs />);
    await screen.findByText("Saving 2 changes");
  });

  it("is not shown when nothing waits", async () => {
    getJobs.mockResolvedValue([]);
    mount(<Jobs />);
    await waitFor(() => expect(getJobs).toHaveBeenCalled());
    expect(screen.queryByRole("status")).toBeNull();
  });

  // Only a job on its way is lost with the tab; one the server has is done regardless.
  it("holds the tab only while a job has not reached the server", async () => {
    getJobs.mockResolvedValue([]);
    const { client } = mount(<Jobs />);
    expect(leave()).toBe(false);

    let reach: (() => void) | undefined;
    new MutationObserver(client, {
      mutationKey: mk.actionsOf("ec_1"),
      mutationFn: () => new Promise<void>((done) => (reach = done)),
    }).mutate([{ ...waiting("j_3"), ref: "r1" }] as never);
    await screen.findByText("Saving a change");
    expect(leave()).toBe(true);

    reach!();
    await waitFor(() => expect(leave()).toBe(false));
  });
});
