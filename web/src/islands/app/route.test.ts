import { describe, expect, it } from "vitest";

import { parse, paths } from "@app/islands/app/route";

describe("a route", () => {
  it("is read from the path", () => {
    expect(parse("/")).toEqual({
      page: "mail",
      config: null,
      mailbox: null,
      message: null,
    });
    expect(parse("/c/ec_1")).toEqual({
      page: "mail",
      config: "ec_1",
      mailbox: null,
      message: null,
    });
    expect(parse("/c/ec_1/mb_2")).toEqual({
      page: "mail",
      config: "ec_1",
      mailbox: "mb_2",
      message: null,
    });
    expect(parse("/settings")).toEqual({ page: "settings", editing: null });
    expect(parse("/settings/")).toEqual({ page: "settings", editing: null });
    expect(parse("/settings/email-configs/new")).toEqual({
      page: "settings",
      editing: { id: null },
    });
    expect(parse("/settings/email-configs/ec_1")).toEqual({
      page: "settings",
      editing: { id: "ec_1" },
    });
  });

  it("is missing rather than a guess when the path is nothing it knows", () => {
    expect(parse("/c/ec_1/mb_2/m_3")).toEqual({
      page: "mail",
      config: "ec_1",
      mailbox: "mb_2",
      message: "m_3",
    });
    for (const path of ["/c", "/c/a/b/c/d", "/settings/x", "/nope"]) {
      expect(parse(path)).toEqual({ page: "missing" });
    }
  });

  it("goes back to where it came from", () => {
    expect(parse(paths.mail("ec_1"))).toEqual({
      page: "mail",
      config: "ec_1",
      mailbox: null,
      message: null,
    });
    expect(parse(paths.mail("ec_1", "mb_2"))).toEqual({
      page: "mail",
      config: "ec_1",
      mailbox: "mb_2",
      message: null,
    });
    expect(paths.mail()).toBe("/");
    expect(paths.mail("ec_1", "mb_2", "m_3")).toBe("/c/ec_1/mb_2/m_3");
    expect(parse(paths.editConfig("ec_1"))).toEqual({
      page: "settings",
      editing: { id: "ec_1" },
    });
    expect(parse(paths.newConfig)).toEqual({
      page: "settings",
      editing: { id: null },
    });
  });
});
