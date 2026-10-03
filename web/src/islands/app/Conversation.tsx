import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import {
  getEmailConfigs,
  getEmailConfigsByIdMailboxesByMailboxMessagesByMessage,
  getEmailConfigsByIdMailboxesByMailboxMessagesByMessageConversation,
} from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Conversation as Talk, Mailbox } from "@app/api/types";
import { Dummy } from "@app/components/Dummy";
import { full, when } from "@app/format";
import { Link } from "@app/islands/app/Link";
import { usePending, withPending } from "@app/islands/app/pending";
import { paths } from "@app/islands/app/route";

/** How many of a conversation show before the rest wait to be asked for. */
const SHOWN = 6;

type Entry = Talk["messages"][number];

/**
 * The conversation the open message is in, oldest first, with the open one marked; any other
 * opens in place to be read, and from there in full. Nothing for a message on its own, or where
 * the server has no THREAD. See docs/reading.md.
 */
export function Conversation({
  config,
  mailbox,
  message,
}: {
  config: string;
  mailbox: Mailbox;
  message: string;
}) {
  const talk = useQuery({
    queryKey: qk.conversation(config, mailbox.id, message),
    queryFn: () =>
      getEmailConfigsByIdMailboxesByMailboxMessagesByMessageConversation(
        config,
        mailbox.id,
        message,
      ),
  });
  const configs = useQuery({
    queryKey: qk.emailConfigs,
    queryFn: getEmailConfigs,
  });
  const self =
    configs.data?.find((c) => c.id === config)?.email.toLowerCase() ?? "";
  const [all, setAll] = useState(false);
  const [peeking, setPeeking] = useState<string | null>(null);
  // Drawn with what was pressed and not yet done: the open message, read on opening, among them.
  const pending = usePending(config);

  const list = (talk.data?.messages ?? []).map((m) =>
    withPending(m, m.mailbox, pending),
  );
  if (list.length < 2) return null;
  const hidden = all ? 0 : Math.max(0, list.length - SHOWN);
  const beyond = talk.data?.earlier ?? 0;
  return (
    <section
      aria-label="Conversation"
      className="max-h-[40vh] shrink-0 overflow-y-auto rounded-md border border-line"
    >
      {hidden > 0 ? (
        <button
          type="button"
          onClick={() => setAll(true)}
          className="w-full px-3 py-1.5 text-left text-xs text-muted hover:bg-fill"
        >
          {hidden === 1 ? "1 earlier message" : `${hidden} earlier messages`}
        </button>
      ) : beyond > 0 ? (
        <p className="px-3 py-1.5 text-xs text-faint">
          {beyond === 1
            ? "1 earlier message is not shown."
            : `${beyond} earlier messages are not shown.`}
        </p>
      ) : null}
      <ul>
        {list.slice(hidden).map((m) => {
          const key = `${m.mailbox} ${m.id}`;
          return (
            <Line
              key={key}
              config={config}
              m={m}
              self={self}
              current={m.mailbox === mailbox.id && m.id === message}
              open={peeking === key}
              onToggle={() => setPeeking((p) => (p === key ? null : key))}
            />
          );
        })}
      </ul>
    </section>
  );
}

function Line({
  config,
  m,
  self,
  current,
  open,
  onToggle,
}: {
  config: string;
  m: Entry;
  self: string;
  current: boolean;
  open: boolean;
  onToggle: () => void;
}) {
  const who =
    m.from.email.toLowerCase() === self
      ? "You"
      : m.from.name || m.from.email || "(no sender)";
  const said = m.sent ?? m.date;
  const row = (
    <>
      <span
        aria-hidden="true"
        className={`size-2 shrink-0 rounded-full ${m.seen ? "" : "bg-brand"}`}
      />
      <span
        className={`w-28 shrink-0 truncate ${m.seen ? "" : "font-semibold"}`}
      >
        {who}
      </span>
      {m.seen ? null : <span className="sr-only">Unread.</span>}
      <span className="min-w-0 flex-1 truncate text-muted">
        {m.preview || m.subject || "(no subject)"}
      </span>
      <time
        dateTime={new Date(said * 1000).toISOString()}
        title={full(said)}
        className="shrink-0 text-xs text-muted"
      >
        {when(said)}
      </time>
    </>
  );
  if (current) {
    return (
      <li
        aria-current="true"
        className="flex items-center gap-2 border-t border-line bg-shade px-3 py-1.5 text-sm first:border-t-0"
      >
        {row}
      </li>
    );
  }
  return (
    <li className="border-t border-line first:border-t-0">
      <button
        type="button"
        aria-expanded={open}
        onClick={onToggle}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-fill"
      >
        {row}
      </button>
      {open ? <Peek config={config} m={m} /> : null}
    </li>
  );
}

/** Another message of the conversation, its text read in place. */
function Peek({ config, m }: { config: string; m: Entry }) {
  const read = useQuery({
    queryKey: qk.message(config, m.mailbox, m.id),
    queryFn: () =>
      getEmailConfigsByIdMailboxesByMailboxMessagesByMessage(
        config,
        m.mailbox,
        m.id,
      ),
  });
  return (
    <div className="flex flex-col gap-2 px-3 pb-3 pl-7">
      {read.data ? (
        <pre className="max-h-64 overflow-y-auto font-sans text-sm whitespace-pre-wrap break-words">
          {(read.data.text || read.data.html_text).trim()}
        </pre>
      ) : read.error ? (
        <p className="text-sm text-accent">{messageOf(read.error)}</p>
      ) : (
        <Dummy className="h-4 w-2/3" />
      )}
      <Link
        href={paths.mail(config, m.mailbox, m.id)}
        className="self-start text-sm text-muted underline"
      >
        Open this message
      </Link>
    </div>
  );
}
