import { useEffect, useRef, useState } from "react";
import { useQuery, type InfiniteData } from "@tanstack/react-query";

import {
  getEmailConfigs,
  getEmailConfigsByIdMailboxesByMailboxMessagesByMessage,
  partURL,
} from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type {
  Address,
  Mailbox,
  Message,
  MessagePage,
  Part,
  ReadMessage,
} from "@app/api/types";
import { Button } from "@app/components/Button";
import { Dummy } from "@app/components/Dummy";
import {
  ForwardMark,
  Paperclip,
  ReplyAllMark,
  ReplyMark,
} from "@app/components/icons";
import { full, size } from "@app/format";
import { Actions } from "@app/islands/app/Actions";
import { forwardOf, others, replyTo, write } from "@app/islands/app/drafts";
import { Frame } from "@app/islands/app/Frame";
import { usePending, withPending } from "@app/islands/app/pending";
import { useCached } from "@app/api/cached";
import { Link } from "@app/islands/app/Link";
import { labelOfMailbox } from "@app/islands/app/mailbox";
import { paths } from "@app/islands/app/route";

/** One message, and what can be done to it. Opening it marks it read, here and on the server. */
export function Reader({
  config,
  mailbox,
  message,
  q = "",
}: {
  config: string;
  mailbox: Mailbox;
  message: string;
  /** What the folder was searched for, if the message was opened from what it found. */
  q?: string;
}) {
  // Images the server held back, in Junk, wait to be asked for, each message afresh. Asking swaps
  // them in where they stand, without fetching the message again.
  const [images, setImages] = useState(false);
  const read = useQuery({
    queryKey: qk.message(config, mailbox.id, message),
    queryFn: () =>
      getEmailConfigsByIdMailboxesByMailboxMessagesByMessage(
        config,
        mailbox.id,
        message,
      ),
  });
  // As the server said, with the actions pressed on it and not yet answered drawn over it.
  const pending = usePending(config);
  const m = read.data && withPending(read.data, mailbox.id, pending);
  // What the list already knows of it — who, what, when, and its flags — drawn while the rest
  // is fetched, so the headers show and the actions work from the moment it is opened.
  const listed = useCached<InfiniteData<MessagePage>>(
    qk.messages(config, mailbox.id, q),
  );
  const row = listed?.pages
    .flatMap((p) => p.messages)
    .find((x) => x.id === message);
  const known: Message | undefined =
    m ?? (row && withPending(row, mailbox.id, pending));

  return (
    <article
      aria-label="Message"
      // HTML scrolls in its frame and the pane holds still around it, one scroll on the screen;
      // a plain message has no frame, and the pane is its scroll.
      className={`relative flex min-w-0 flex-1 flex-col overflow-x-hidden bg-bg ${m?.html ? "overflow-y-hidden" : "overflow-y-auto"}`}
    >
      <div className="flex shrink-0 flex-col gap-2 border-b border-line px-4 py-4">
        <Actions
          config={config}
          mailbox={mailbox}
          message={message}
          q={q}
          m={known}
          back={
            <Link
              href={paths.mail(config, mailbox.id, undefined, q)}
              className="text-sm text-muted lg:hidden"
            >
              ← {labelOfMailbox(mailbox)}
            </Link>
          }
        />
        {known ? (
          <>
            <h2 className="text-xl font-semibold break-words">
              {known.subject || "(no subject)"}
            </h2>
            <div className="flex flex-wrap items-baseline gap-x-2 text-sm">
              <span className="font-medium">
                {known.from.name || known.from.email || "(no sender)"}
              </span>
              {known.from.name && known.from.email ? (
                <span className="text-muted">&lt;{known.from.email}&gt;</span>
              ) : null}
              <span className="flex-1" />
              <time
                dateTime={new Date(known.date * 1000).toISOString()}
                className="text-xs text-muted"
              >
                {full(known.date)}
              </time>
            </div>
            <People label="To" people={known.to} />
            <People label="Cc" people={m?.cc ?? []} />
          </>
        ) : read.error ? null : (
          <div className="flex flex-col gap-2">
            <Dummy className="h-6 w-2/3" />
            <Dummy className="h-4 w-1/2" />
            <Dummy className="h-4 w-1/3" />
          </div>
        )}
        {read.error ? (
          <p className="text-sm text-accent">{messageOf(read.error)}</p>
        ) : null}
      </div>

      {!m && !read.error ? (
        <div className="flex flex-col gap-2 px-4 py-4">
          <Dummy className="h-4 w-full" />
          <Dummy className="h-4 w-5/6" />
          <Dummy className="h-4 w-2/3" />
        </div>
      ) : null}

      {m ? (
        <>
          <Attachments
            config={config}
            mailbox={mailbox.id}
            message={m.id}
            parts={m.parts}
          />
          {m.held_images > 0 && !images ? (
            <div className="flex shrink-0 flex-wrap items-center gap-3 border-b border-line bg-fill px-4 py-2 text-sm">
              <span className="text-muted">
                {m.held_images === 1
                  ? "1 image from the internet is not shown."
                  : `${m.held_images} images from the internet are not shown.`}
              </span>
              <Button size="bar" onClick={() => setImages(true)}>
                Show images
              </Button>
            </div>
          ) : null}
          {m.html ? (
            <Frame html={m.html} images={images} />
          ) : (
            <pre className="shrink-0 px-4 py-4 font-sans text-sm whitespace-pre-wrap break-words">
              {m.text}
            </pre>
          )}
          <Replies config={config} m={m} />
        </>
      ) : null}
    </article>
  );
}

/**
 * The ways to answer it, along the bottom of the pane: under a frame that scrolls itself, and
 * kept in view while a plain message scrolls the pane. Gmail's keys too: r, a and f.
 */
function Replies({ config, m }: { config: string; m: ReadMessage }) {
  const configs = useQuery({
    queryKey: qk.emailConfigs,
    queryFn: getEmailConfigs,
  });
  const self = configs.data?.find((c) => c.id === config)?.email ?? "";
  const answer = {
    r: () => write(replyTo(m, config, self, false)),
    a: others(m, self)
      ? () => write(replyTo(m, config, self, true))
      : undefined,
    f: () => write(forwardOf(m, config)),
  };

  const latest = useRef(answer);
  latest.current = answer;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const el = e.target instanceof HTMLElement ? e.target : null;
      if (el?.closest("input, textarea, select, [contenteditable='true']"))
        return;
      if (document.querySelector("dialog[open]")) return;
      const run = latest.current[e.key as "r" | "a" | "f"];
      if (!run) return;
      e.preventDefault();
      run();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div className="sticky bottom-0 mt-auto flex shrink-0 flex-wrap gap-2 border-t border-line bg-bg px-4 py-2">
      <Button size="bar" title="Reply (r)" onClick={answer.r}>
        <ReplyMark />
        Reply
      </Button>
      {answer.a ? (
        <Button size="bar" title="Reply all (a)" onClick={answer.a}>
          <ReplyAllMark />
          Reply all
        </Button>
      ) : null}
      <Button size="bar" title="Forward (f)" onClick={answer.f}>
        <ForwardMark />
        Forward
      </Button>
    </div>
  );
}

function People({ label, people }: { label: string; people: Address[] }) {
  if (people.length === 0) return null;
  return (
    <p className="text-sm text-muted">
      {label}:{" "}
      {people.map((p, i) => (
        <span key={i} title={p.email}>
          {i > 0 ? ", " : ""}
          {p.name || p.email}
        </span>
      ))}
    </p>
  );
}

function Attachments({
  config,
  mailbox,
  message,
  parts,
}: {
  config: string;
  mailbox: string;
  message: string;
  parts: Part[];
}) {
  const listed = parts.filter((p) => p.listed);
  if (listed.length === 0) return null;
  return (
    <ul
      aria-label="Attachments"
      className="flex shrink-0 flex-wrap gap-2 border-b border-line px-4 py-3"
    >
      {listed.map((p) => (
        <li key={p.section}>
          <a
            href={partURL(config, mailbox, message, p.section)}
            download={p.name}
            className="inline-flex items-center gap-1.5 rounded-md border border-line bg-bg px-2.5 py-1 text-sm hover:bg-fill"
          >
            <Paperclip className="shrink-0 text-muted" />
            <span className="max-w-56 truncate">{p.name}</span>
            <span className="text-xs text-muted">{size(p.size)}</span>
          </a>
        </li>
      ))}
    </ul>
  );
}
