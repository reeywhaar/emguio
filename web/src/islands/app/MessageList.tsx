import { useEffect, useRef } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";

import { getEmailConfigsByIdMailboxesByMailboxMessages } from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Mailbox, Message } from "@app/api/types";
import { Button } from "@app/components/Button";
import { Dummy } from "@app/components/Dummy";
import { Paperclip, Star } from "@app/components/icons";
import { full, when } from "@app/format";
import { Link } from "@app/islands/app/Link";
import { counterpart } from "@app/islands/app/mailbox";
import { listWithPending, usePending } from "@app/islands/app/pending";
import { paths } from "@app/islands/app/route";

/** How far before the end of the list the next run starts loading. */
const AHEAD = 200;

/** Messages being selected, and a way to change which. */
export type Selecting = {
  selected: Set<string>;
  change: (next: Set<string>) => void;
};

/**
 * One folder's messages, newest to arrive first, reaching further back as it is scrolled; with
 * q, those the mail server found in it for q.
 */
export function MessageList({
  config,
  mailbox,
  open,
  q = "",
  select,
}: {
  config: string;
  mailbox: Mailbox;
  /** The message open beside the list, if any. */
  open: string | null;
  q?: string;
  /** While selecting: a row is a checkbox rather than a way to open the message. */
  select?: Selecting;
}) {
  const pending = usePending(config);
  const pages = useInfiniteQuery({
    queryKey: qk.messages(config, mailbox.id, q),
    queryFn: ({ pageParam }) =>
      getEmailConfigsByIdMailboxesByMailboxMessages(
        config,
        mailbox.id,
        pageParam,
        q,
      ),
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor,
  });

  // Measured on the list's own scroll box: a margin on the window would not reach past its edge.
  const scroller = useRef<HTMLDivElement>(null);
  const end = useRef<HTMLDivElement>(null);
  const {
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = pages;
  useEffect(() => {
    const node = end.current;
    if (!node || !hasNextPage || isFetchNextPageError) return;
    const watch = new IntersectionObserver(
      (entries) => {
        // A read of the whole list underway is let finish rather than cut short: the next run
        // is asked for from what it brings.
        if (entries.some((e) => e.isIntersecting) && !isFetchingNextPage) {
          fetchNextPage({ cancelRefetch: false });
        }
      },
      { root: scroller.current, rootMargin: `0px 0px ${AHEAD}px 0px` },
    );
    watch.observe(node);
    return () => watch.disconnect();
  }, [
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
    pages.data,
  ]);

  // Where a Shift-click's range starts: the last row toggled.
  const anchor = useRef<string | null>(null);

  const selecting = Boolean(select);
  useEffect(() => {
    if (selecting) scroller.current?.querySelector("input")?.focus();
  }, [selecting]);

  if (!pages.data) {
    if (pages.error) {
      return (
        <p className="p-4 text-sm text-accent">{messageOf(pages.error)}</p>
      );
    }
    return <MessageDummies count={4} />;
  }
  const messages = listWithPending(
    pages.data.pages.flatMap((p) => p.messages),
    mailbox.id,
    pending,
  );

  // A click toggles one row; a Shift-click sets every row from the last one toggled to this one
  // the way this one goes.
  const toggle = (id: string, range: boolean) => {
    if (!select) return;
    const next = new Set(select.selected);
    const on = !next.has(id);
    const from =
      range && anchor.current
        ? messages.findIndex((m) => m.id === anchor.current)
        : -1;
    const to = messages.findIndex((m) => m.id === id);
    const run =
      from >= 0
        ? messages.slice(Math.min(from, to), Math.max(from, to) + 1)
        : [messages[to]!];
    for (const m of run) {
      if (on) next.add(m.id);
      else next.delete(m.id);
    }
    anchor.current = id;
    select.change(next);
  };

  return (
    // relative, so what is absolutely positioned inside the list — the words only a screen
    // reader hears — is placed in the list rather than down the page, where it stretched the
    // window past the bottom of the screen.
    <div ref={scroller} className="relative min-h-0 flex-1 overflow-y-auto">
      {messages.length > 0 ? (
        <ul aria-label="Messages">
          {messages.map((m) => (
            <Row
              key={m.id}
              config={config}
              mailbox={mailbox}
              message={m}
              q={q}
              open={m.id === open}
              selected={select ? select.selected.has(m.id) : undefined}
              onToggle={select ? (range) => toggle(m.id, range) : undefined}
            />
          ))}
        </ul>
      ) : hasNextPage ? null : (
        <p className="p-4 text-sm text-muted">
          {q ? "Nothing here matches." : "No messages here."}
        </p>
      )}
      <div ref={end}>
        {pages.error ? (
          <div className="flex flex-col items-center gap-2 p-4 text-center">
            <p className="text-sm text-accent">{messageOf(pages.error)}</p>
            <Button
              size="bar"
              onClick={() =>
                isFetchNextPageError ? fetchNextPage() : pages.refetch()
              }
            >
              Try again
            </Button>
          </div>
        ) : hasNextPage ? (
          <div role="status">
            <span className="sr-only">Loading older messages.</span>
            <MessageDummies count={2} />
          </div>
        ) : null}
      </div>
    </div>
  );
}

/** Rows standing in for messages not here yet, a message's height each. */
export function MessageDummies({ count }: { count: number }) {
  return (
    <ul aria-hidden="true">
      {Array.from({ length: count }, (_, i) => (
        <li
          key={i}
          className="flex items-start gap-3 border-b border-line px-4 py-2.5"
        >
          <span className="mt-1.5 size-2 shrink-0" />
          <div className="min-w-0 flex-1">
            <div className="flex h-5 items-center gap-2">
              <Dummy className="h-3.5 w-32" />
              <span className="flex-1" />
              <Dummy className="h-3 w-12" />
            </div>
            <div className="flex h-5 items-center">
              <Dummy className="h-3.5 w-56 max-w-full" />
            </div>
            <div className="flex h-5 items-center">
              <Dummy className="h-3.5 w-72 max-w-full" />
            </div>
          </div>
        </li>
      ))}
    </ul>
  );
}

function Row({
  config,
  mailbox,
  message: m,
  q,
  open,
  selected,
  onToggle,
}: {
  config: string;
  mailbox: Mailbox;
  message: Message;
  q: string;
  open: boolean;
  /** Set while selecting: whether this row is. */
  selected?: boolean;
  onToggle?: (range: boolean) => void;
}) {
  const unread = !m.seen;
  const dot = (
    <span
      aria-hidden="true"
      className={`mt-1.5 size-2 shrink-0 rounded-full ${unread ? "bg-brand" : ""}`}
    />
  );
  const look = `flex items-start gap-3 px-4 py-2.5 ${open || selected ? "bg-shade" : "bg-bg hover:bg-fill"}`;
  const body = (
    <div className="min-w-0 flex-1">
      <div className="flex items-baseline gap-2">
        {unread ? <span className="sr-only">Unread.</span> : null}
        <span
          className={`min-w-0 flex-1 truncate text-sm ${unread ? "font-semibold" : ""}`}
        >
          {counterpart(mailbox, m)}
        </span>
        {m.has_attachments ? (
          <Paperclip className="shrink-0 self-center text-muted" />
        ) : null}
        {m.flagged ? <Star className="shrink-0 self-center text-warn" /> : null}
        <time
          dateTime={new Date(m.date * 1000).toISOString()}
          title={full(m.date)}
          className="shrink-0 text-xs text-muted"
        >
          {when(m.date)}
        </time>
      </div>
      <p className={`truncate text-sm ${unread ? "text-fg" : "text-muted"}`}>
        {m.subject || "(no subject)"}
      </p>
      {m.preview ? (
        <p className="truncate text-sm text-faint">{m.preview}</p>
      ) : null}
    </div>
  );
  return (
    <li className="border-b border-line">
      {onToggle ? (
        <label className={`${look} cursor-pointer select-none`}>
          <input
            type="checkbox"
            checked={selected}
            readOnly
            onClick={(e) => onToggle(e.shiftKey)}
            aria-label={m.subject || "(no subject)"}
            className="mt-1 size-4 shrink-0 accent-brand"
          />
          {dot}
          {body}
        </label>
      ) : (
        <Link
          href={paths.mail(config, mailbox.id, m.id, q)}
          aria-current={open ? "true" : undefined}
          className={look}
        >
          {dot}
          {body}
        </Link>
      )}
    </li>
  );
}
