import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { prefixes, Subject, untitled } from "@app/islands/app/Subject";

/** The words a subject is drawn as, the badges' included. */
const shown = (subject: string) =>
  render(<Subject subject={subject} />).container.textContent;

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

  it("counts each run of Re: and Fwd: it starts with", () => {
    expect(prefixes("Re: Re: Re: Lunch")).toEqual({
      runs: [{ kind: "Re", count: 3 }],
      rest: "Lunch",
    });
    expect(prefixes("Fwd: RE: re:Lunch")).toEqual({
      runs: [
        { kind: "Fwd", count: 1 },
        { kind: "Re", count: 2 },
      ],
      rest: "Lunch",
    });
    expect(prefixes("Re[2]: Re: Lunch").runs).toEqual([
      { kind: "Re", count: 3 },
    ]);
    expect(prefixes("Lunch")).toEqual({ runs: [], rest: "Lunch" });
  });

  it("shows a run as one, with how many in a badge", () => {
    expect(shown("Re: Re: Re: Lunch")).toBe("Re3: Lunch");
    expect(shown("Fwd: Re: Re: Lunch")).toBe("Fwd: Re2: Lunch");
    expect(shown("RE: Lunch")).toBe("RE: Lunch");
    expect(shown("Re: Re: (No subject)")).toBe("No subject");
    expect(shown("Fwd: Fwd: Fwd: Report")).toBe("Fwd3: Report");
  });
});
