import { describe, expect, it } from "vitest";

import { ago, full, size, when } from "@app/format";

const now = new Date(2026, 9, 1, 15, 30);
const unix = (d: Date) => d.getTime() / 1000;

describe("a date in a list", () => {
  it("is a time today, on a 24-hour clock, and the day otherwise, as ISO writes it", () => {
    expect(when(unix(new Date(2026, 9, 1, 21, 5)), now)).toBe("21:05");
    expect(when(unix(new Date(2026, 2, 4, 9, 5)), now)).toBe("2026-03-04");
    expect(when(unix(new Date(2025, 11, 31, 9, 5)), now)).toBe("2025-12-31");
  });
});

describe("a date in full", () => {
  it("is its weekday, its day as ISO writes it, and its time on a 24-hour clock", () => {
    expect(full(unix(new Date(2026, 2, 4, 21, 5)))).toMatch(
      /^\S+ 2026-03-04 21:05$/,
    );
  });
});

describe("how long ago", () => {
  it("is in words", () => {
    expect(ago(unix(now) - 10, now)).toBe("just now");
    expect(ago(unix(now) - 5 * 60, now)).toMatch(/5 minutes ago/);
    expect(ago(unix(now) - 3 * 3600, now)).toMatch(/3 hours ago/);
  });
});

describe("a size", () => {
  it("is in the unit that keeps it short", () => {
    expect(size(512)).toBe("512 B");
    expect(size(12_800)).toBe("13 KB");
    expect(size(5_400_000)).toBe("5.1 MB");
  });
});
