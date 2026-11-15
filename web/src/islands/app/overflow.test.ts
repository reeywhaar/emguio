import { describe, expect, it } from "vitest";

import { fit } from "@app/islands/app/overflow";

const widths = { a: 100, b: 100, c: 100, d: 100 };
const ids = ["a", "b", "c", "d"];

describe("fit", () => {
  it("shows them all when they fit", () => {
    expect(fit({ ids, widths, room: 412, button: 30, gap: 4 })).toEqual(ids);
  });

  it("shows those that fit beside the button, in order", () => {
    // 100 + 4 + 100 + 4 + 30 = 238
    expect(fit({ ids, widths, room: 238, button: 30, gap: 4 })).toEqual([
      "a",
      "b",
    ]);
    expect(fit({ ids, widths, room: 237, button: 30, gap: 4 })).toEqual(["a"]);
  });

  it("keeps the current one, in its place", () => {
    expect(
      fit({ ids, widths, room: 238, button: 30, gap: 4, current: "d" }),
    ).toEqual(["a", "d"]);
  });

  it("shows none when not even the current one fits beside the button", () => {
    expect(
      fit({ ids, widths, room: 120, button: 30, gap: 4, current: "c" }),
    ).toEqual([]);
  });
});
