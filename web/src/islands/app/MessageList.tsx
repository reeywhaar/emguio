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

/** One folder's messages, newest to arrive first, reaching further back as it is scrolled. */
export function MessageList({
  config,
  mailbox,
  open,
}: {
  config: string;
  mailbox: Mailbox;
  /** The message open beside the list, if any. */
  open: string | null;
}) {
  const pending = usePending(config);
  const pages = useInfiniteQuery({
    queryKey: qk.messages(config, mailbox.id),
    queryFn: ({ pageParam }) =>
      getEmailConfigsByIdMailboxesByMailboxMessages(
        config,
        mailbox.id,
        pageParam,
      ),
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor,
  });

  // The next run loads as the end of the list comes into view. Where the browser cannot say
  // when that is, the button below does it instead.
  const end = useRef<HTMLDivElement>(null);
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = pages;
  useEffect(() => {
    const node = end.current;
    if (!node || !hasNextPage || typeof IntersectionObserver === "undefined")
      return;
    const watch = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting) && !isFetchingNextPage)
        fetchNextPage();
    });
    watch.observe(node);
    return () => watch.disconnect();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  if (pages.error) {
    return <p className="p-4 text-sm text-accent">{messageOf(pages.error)}</p>;
  }
  if (!pages.data) {
    return (
      <div className="flex flex-col gap-3 p-4">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="flex flex-col gap-1.5">
            <Dummy className="h-4 w-48" />
            <Dummy className="h-4 w-80 max-w-full" />
          </div>
        ))}
      </div>
    );
  }
  const messages = listWithPending(
    pages.data.pages.flatMap((p) => p.messages),
    mailbox.id,
    pending,
  );
  if (messages.length === 0) {
    return <p className="p-4 text-sm text-muted">No messages here.</p>;
  }

  return (
    // relative, so what is absolutely positioned inside the list — the words only a screen
    // reader hears — is placed in the list rather than down the page, where it stretched the
    // window past the bottom of the screen.
    <div className="relative min-h-0 flex-1 overflow-y-auto">
      <ul aria-label="Messages">
        {messages.map((m) => (
          <Row
            key={m.id}
            config={config}
            mailbox={mailbox}
            message={m}
            open={m.id === open}
          />
        ))}
      </ul>
      <div ref={end} className="flex justify-center p-3">
        {hasNextPage ? (
          <Button
            size="bar"
            disabled={isFetchingNextPage}
            onClick={() => fetchNextPage()}
          >
            {isFetchingNextPage ? "Loading" : "Show older"}
          </Button>
        ) : null}
      </div>
    </div>
  );
}

function Row({
  config,
  mailbox,
  message: m,
  open,
}: {
  config: string;
  mailbox: Mailbox;
  message: Message;
  open: boolean;
}) {
  const unread = !m.seen;
  return (
    <li className="border-b border-line">
      <Link
        href={paths.mail(config, mailbox.id, m.id)}
        aria-current={open ? "true" : undefined}
        className={`flex items-start gap-3 px-4 py-2.5 ${open ? "bg-shade" : "bg-bg hover:bg-fill"}`}
      >
        <span
          aria-hidden="true"
          className={`mt-1.5 size-2 shrink-0 rounded-full ${unread ? "bg-brand" : ""}`}
        />
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
            {m.flagged ? (
              <Star className="shrink-0 self-center text-warn" />
            ) : null}
            <time
              dateTime={new Date(m.date * 1000).toISOString()}
              title={full(m.date)}
              className="shrink-0 text-xs text-muted"
            >
              {when(m.date)}
            </time>
          </div>
          <p
            className={`truncate text-sm ${unread ? "text-fg" : "text-muted"}`}
          >
            {m.subject || "(no subject)"}
          </p>
          {m.preview ? (
            <p className="truncate text-sm text-faint">{m.preview}</p>
          ) : null}
        </div>
      </Link>
    </li>
  );
}
