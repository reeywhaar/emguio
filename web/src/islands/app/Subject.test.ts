import { describe, expect, it } from "vitest";

import { untitled } from "@app/islands/app/Subject";

describe("a subject", () => {
  it("is none when empty, or only a placeholder after any replies and forwards", () => {
    for (const subject of [
      "",
      "  ",
      "Re:",
      "(no subject)",
      "Re: (No subject)",
      "Re: Re: Re: No Subject",
      "RE[2]: [No Subject]",
      "Fwd: No subject",
      "Fwd: Re: no subject",
      "Fw: Без темы",
      "Re: (Без темы)",
    ]) {
      expect(untitled(subject), subject).toBe(true);
    }
  });

  it("is a subject when it says anything else", () => {
    for (const subject of [
      "Lunch",
      "Re: Lunch",
      "No subject to the rule",
      "Re: no subjects today",
      "Report (no subject line)",
    ]) {
      expect(untitled(subject), subject).toBe(false);
    }
  });
});
