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

  it("go no further than the server holds mail", () => {
    const now = new Date(2026, 10, 11, 10, 30);
    expect(usual(now, at(now) + 2 * 3600).map((t) => t.at)).toEqual([
      at(now) + 3600,
    ]);
  });
});
