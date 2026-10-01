import type { Check, TestResult } from "@app/api/types";

/** What each server said to a test, in the server's own words when it refused. */
export function Results({ result }: { result: TestResult }) {
  return (
    <ul className="flex flex-col gap-1 text-sm" aria-live="polite">
      <Line label="Incoming" check={result.incoming} />
      {result.outgoing ? (
        <Line label="Outgoing" check={result.outgoing} />
      ) : null}
    </ul>
  );
}

function Line({ label, check }: { label: string; check: Check }) {
  return (
    <li className={check.ok ? "text-ok" : "text-accent"}>
      {label}: {check.ok ? "signed in." : check.message}
    </li>
  );
}
