import { useEffect, useRef, useState } from "react";

import type { Mailbox } from "@app/api/types";
import { Cross } from "@app/components/icons";
import { fieldLook } from "@app/components/TextField";
import { labelOfMailbox } from "@app/islands/app/mailbox";
import { go, paths } from "@app/islands/app/route";

/** The folder's search field. Keyed by the query where it is drawn, so it starts with it. */
export function Search({
  config,
  mailbox,
  q,
}: {
  config: string;
  mailbox: Mailbox;
  q: string;
}) {
  const [draft, setDraft] = useState(q);
  const field = useRef<HTMLInputElement>(null);
  const label = `Search ${labelOfMailbox(mailbox)}`;

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey) return;
      const el = e.target instanceof HTMLElement ? e.target : null;
      if (el?.closest("input, textarea, select, [contenteditable]")) return;
      if (document.querySelector("dialog[open]")) return;
      e.preventDefault();
      field.current?.focus();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const look = (query: string) =>
    go(paths.mail(config, mailbox.id, undefined, query.trim()));
  const clear = () => {
    setDraft("");
    if (q) look("");
  };

  return (
    <form
      role="search"
      className="border-b border-line bg-bg px-4 py-2"
      onSubmit={(e) => {
        e.preventDefault();
        look(draft);
      }}
    >
      <div className="relative">
        <input
          ref={field}
          type="search"
          enterKeyHint="search"
          aria-label={label}
          placeholder={label}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key !== "Escape" || (!draft && !q)) return;
            // The folder's own Escape, which leaves selecting, is not this one.
            e.preventDefault();
            e.stopPropagation();
            clear();
          }}
          className={`${fieldLook} min-h-8 w-full py-1 pr-9 pointer-coarse:min-h-10 [&::-webkit-search-cancel-button]:appearance-none`}
        />
        {draft || q ? (
          <button
            type="button"
            aria-label="Clear the search"
            title="Clear the search (Esc)"
            onClick={clear}
            className="absolute inset-y-0 right-0 flex w-9 items-center justify-center text-muted hover:text-fg"
          >
            <Cross />
          </button>
        ) : null}
      </div>
    </form>
  );
}
