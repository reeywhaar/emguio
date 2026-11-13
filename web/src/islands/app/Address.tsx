import { useEffect, useState } from "react";

/** An address as a badge: a press copies it, and it says so for a moment. */
export function CopyAddress({ email }: { email: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const done = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(done);
  }, [copied]);
  return (
    <span className="inline-flex max-w-full min-w-0">
      <button
        type="button"
        title="Copy"
        aria-label={`Copy ${email}`}
        className="relative inline-grid max-w-full min-w-0 rounded border border-line px-1.5 text-xs leading-5 font-normal text-muted hover:bg-fill hover:text-fg"
        onClick={() =>
          navigator.clipboard?.writeText(email).then(
            () => setCopied(true),
            () => {},
          )
        }
      >
        {/* The address keeps the badge its width while it says Copied. */}
        <span className={`truncate ${copied ? "invisible" : ""}`}>{email}</span>
        {copied ? (
          <span className="absolute inset-0 grid place-items-center text-fg">
            Copied
          </span>
        ) : null}
      </button>
      <span className="sr-only" aria-live="polite">
        {copied ? "Copied" : ""}
      </span>
    </span>
  );
}
