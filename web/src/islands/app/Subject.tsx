import { Fragment } from "react";

import { Tag } from "@app/components/Tag";

/**
 * No subject at all, or the placeholder a client writes in for one — "(No subject)", "No
 * Subject", "Без темы" — after any Re: or Fwd:, however many.
 */
const none =
  /^(?:(?:re|fwd?)(?:\[\d+\])?\s*:\s*)*(?:[([]?\s*(?:no subject|без темы)\s*[)\]]?)?$/i;

/** Whether a message has no subject, or only a placeholder for one. */
export const untitled = (subject: string) => none.test(subject.trim());

/** One Re: or Fwd: at the start of a subject, with the count some clients write: Re[2]:. */
const prefix = /^(re|fwd?)(?:\[(\d+)\])?\s*:\s*/i;

type Run = { kind: "Re" | "Fwd"; count: number };

/** The Re: and Fwd: a subject starts with, each run of one kind counted, and what follows. */
export function prefixes(subject: string): { runs: Run[]; rest: string } {
  const runs: Run[] = [];
  let rest = subject.trim();
  for (let m = prefix.exec(rest); m; m = prefix.exec(rest)) {
    const kind = m[1]!.toLowerCase() === "re" ? "Re" : "Fwd";
    const count = m[2] ? Number(m[2]) : 1;
    const last = runs.at(-1);
    if (last?.kind === kind) last.count += count;
    else runs.push({ kind, count });
    rest = rest.slice(m[0].length);
  }
  return { runs, rest };
}

/**
 * A message's subject, or a mark that it has none rather than words standing in for one. A run of
 * Re: is one, with how many in a badge: Re: Re: Re: is Re ③:.
 */
export function Subject({ subject }: { subject: string }) {
  if (untitled(subject)) return <Tag>No subject</Tag>;
  const { runs, rest } = prefixes(subject);
  if (runs.every((r) => r.count === 1)) return <>{subject}</>;
  return (
    <>
      {runs.map((r, i) => (
        <Fragment key={i}>
          {r.kind}
          {r.count > 1 ? <Tag className="ml-0.5">{r.count}</Tag> : null}
          {": "}
        </Fragment>
      ))}
      {rest}
    </>
  );
}
