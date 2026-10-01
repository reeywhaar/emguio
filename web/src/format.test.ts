import { describe, expect, it } from "vitest";

import { ago, size, when } from "@app/format";

const now = new Date(2026, 9, 1, 15, 30);
const unix = (d: Date) => d.getTime() / 1000;

describe("a date in a list", () => {
  it("is a time today, a day this year, and a date before", () => {
    expect(when(unix(new Date(2026, 9, 1, 9, 5)), now)).toMatch(/09.05/);
    expect(when(unix(new Date(2026, 2, 4, 9, 5)), now)).not.toMatch(/2026/);
    expect(when(unix(new Date(2025, 2, 4, 9, 5)), now)).toMatch(/2025/);
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
