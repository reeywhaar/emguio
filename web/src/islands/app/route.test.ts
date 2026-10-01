import { describe, expect, it } from "vitest";

import { parse, paths } from "@app/islands/app/route";

describe("a route", () => {
  it("is read from the path", () => {
    expect(parse("/")).toEqual({
      page: "mail",
      config: null,
      mailbox: null,
      message: null,
      q: "",
    });
    expect(parse("/c/ec_1")).toEqual({
      page: "mail",
      config: "ec_1",
      mailbox: null,
      message: null,
      q: "",
    });
    expect(parse("/c/ec_1/mb_2")).toEqual({
      page: "mail",
      config: "ec_1",
      mailbox: "mb_2",
      message: null,
      q: "",
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
      q: "",
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
      q: "",
    });
    expect(parse(paths.mail("ec_1", "mb_2"))).toEqual({
      page: "mail",
      config: "ec_1",
      mailbox: "mb_2",
      message: null,
      q: "",
    });
    expect(paths.mail()).toBe("/");
    expect(paths.mail("ec_1", "mb_2", "m_3")).toBe("/c/ec_1/mb_2/m_3");
  });

  // What a folder is searched for rides along, so a result opened, a reload or Back keeps it.
  it("keeps what a folder is searched for", () => {
    const at = paths.mail("ec_1", "mb_2", "m_3", "from:alice lunch");
    expect(at).toBe("/c/ec_1/mb_2/m_3?q=from%3Aalice+lunch");
    expect(parse(at)).toEqual({
      page: "mail",
      config: "ec_1",
      mailbox: "mb_2",
      message: "m_3",
      q: "from:alice lunch",
    });
    // Only a folder is searched.
    expect(paths.mail("ec_1", undefined, undefined, "lunch")).toBe("/c/ec_1");
    expect(parse("/c/ec_1?q=lunch")).toMatchObject({ mailbox: null, q: "" });
  });

  it("names settings by their path", () => {
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
