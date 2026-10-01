import { describe, expect, it } from "vitest";

import { parse, paths } from "@app/islands/app/route";

describe("a route", () => {
  it("is read from the path", () => {
    expect(parse("/")).toEqual({ page: "mail", config: null, mailbox: null });
    expect(parse("/c/ec_1")).toEqual({
      page: "mail",
      config: "ec_1",
      mailbox: null,
    });
    expect(parse("/c/ec_1/mb_2")).toEqual({
      page: "mail",
      config: "ec_1",
      mailbox: "mb_2",
    });
    expect(parse("/settings")).toEqual({ page: "settings" });
    expect(parse("/settings/")).toEqual({ page: "settings" });
    expect(parse("/settings/email-configs/new")).toEqual({
      page: "new-config",
    });
    expect(parse("/settings/email-configs/ec_1")).toEqual({
      page: "edit-config",
      id: "ec_1",
    });
  });

  it("is missing rather than a guess when the path is nothing it knows", () => {
    for (const path of ["/c", "/c/a/b/c", "/settings/x", "/nope"]) {
      expect(parse(path)).toEqual({ page: "missing" });
    }
  });

  it("goes back to where it came from", () => {
    expect(parse(paths.mail("ec_1"))).toEqual({
      page: "mail",
      config: "ec_1",
      mailbox: null,
    });
    expect(parse(paths.mail("ec_1", "mb_2"))).toEqual({
      page: "mail",
      config: "ec_1",
      mailbox: "mb_2",
    });
    expect(parse(paths.editConfig("ec_1"))).toEqual({
      page: "edit-config",
      id: "ec_1",
    });
    expect(parse(paths.newConfig)).toEqual({ page: "new-config" });
  });
});
