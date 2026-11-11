import { describe, expect, it } from "vitest";

import { usual } from "@app/islands/app/SendLater";

const at = (d: Date) => Math.floor(d.getTime() / 1000);

describe("the usual times to send later", () => {
  // A Wednesday morning: in an hour, tomorrow at 8, and Monday at 8.
  it("are an hour on, tomorrow morning and Monday morning", () => {
    const now = new Date(2026, 10, 11, 10, 30);
    expect(usual(now, at(now) + 30 * 86400).map((t) => t.at)).toEqual([
      at(now) + 3600,
      at(new Date(2026, 10, 12, 8, 0)),
      at(new Date(2026, 10, 16, 8, 0)),
    ]);
  });

  // On a Sunday Monday is tomorrow, said once.
  it("say Monday once when it is tomorrow", () => {
    const now = new Date(2026, 10, 15, 21, 0);
    expect(usual(now, at(now) + 30 * 86400)).toHaveLength(2);
  });

  // The minutes go in fives, so an hour on is the next five after it.
  it("put an hour on at the next five minutes", () => {
    const now = new Date(2026, 10, 11, 10, 32);
    expect(usual(now, at(now) + 30 * 86400)[0]!.at).toBe(
      at(new Date(2026, 10, 11, 11, 35)),
    );
  });

  it("go no further than the server holds mail", () => {
    const now = new Date(2026, 10, 11, 10, 30);
    expect(usual(now, at(now) + 2 * 3600).map((t) => t.at)).toEqual([
      at(now) + 3600,
    ]);
  });
});
