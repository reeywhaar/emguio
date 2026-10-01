import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import {
  getEmailConfigsByIdMessagesByMessage,
  partURL,
} from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Address, Mailbox } from "@app/api/types";
import { Button } from "@app/components/Button";
import { Dummy } from "@app/components/Dummy";
import { Paperclip } from "@app/components/icons";
import { full, size } from "@app/format";
import { Frame } from "@app/islands/app/Frame";
import { Link } from "@app/islands/app/Link";
import { labelOfMailbox } from "@app/islands/app/mailbox";
import { paths } from "@app/islands/app/route";

/** One message, whole. Reading it here leaves it unread on the server. */
export function Reader({
  config,
  mailbox,
  message,
}: {
  config: string;
  mailbox: Mailbox;
  message: string;
}) {
  // Remote images are a request the sender sees, so they wait to be asked for, each message
  // afresh.
  const [images, setImages] = useState(false);
  const read = useQuery({
    queryKey: qk.message(config, message, images),
    queryFn: () =>
      getEmailConfigsByIdMessagesByMessage(config, message, images),
    // What was shown stays while the version with images arrives, rather than the pane going
    // blank between the two.
    placeholderData: (previous) => previous,
  });
  const m = read.data;

  return (
    <article
      aria-label="Message"
      className="relative flex min-w-0 flex-1 flex-col overflow-y-auto bg-bg"
    >
      <div className="flex flex-col gap-2 border-b border-line px-4 py-4 sm:px-6">
        <Link
          href={paths.mail(config, mailbox.id)}
          className="text-sm text-muted lg:hidden"
        >
          ← {labelOfMailbox(mailbox)}
        </Link>
        {read.error ? (
          <p className="text-sm text-accent">{messageOf(read.error)}</p>
        ) : !m ? (
          <div className="flex flex-col gap-2">
            <Dummy className="h-6 w-2/3" />
            <Dummy className="h-4 w-1/2" />
            <Dummy className="h-4 w-1/3" />
          </div>
        ) : (
          <>
            <h2 className="text-xl font-semibold break-words">
              {m.subject || "(no subject)"}
            </h2>
            <div className="flex flex-wrap items-baseline gap-x-2 text-sm">
              <span className="font-medium">
                {m.from.name || m.from.email || "(no sender)"}
              </span>
              {m.from.name && m.from.email ? (
                <span className="text-muted">&lt;{m.from.email}&gt;</span>
              ) : null}
              <span className="flex-1" />
              <time
                dateTime={new Date(m.date * 1000).toISOString()}
                className="text-xs text-muted"
              >
                {full(m.date)}
              </time>
            </div>
            <People label="To" people={m.to} />
            <People label="Cc" people={m.cc} />
          </>
        )}
      </div>

      {m ? (
        <>
          <Attachments config={config} message={m.id} parts={m.parts} />
          {m.remote_images > 0 && !images ? (
            <div className="flex flex-wrap items-center gap-3 border-b border-line bg-fill px-4 py-2 text-sm sm:px-6">
              <span className="text-muted">
                {m.remote_images === 1
                  ? "1 image from the internet is not shown."
                  : `${m.remote_images} images from the internet are not shown.`}
              </span>
              <Button size="bar" onClick={() => setImages(true)}>
                Show images
              </Button>
            </div>
          ) : null}
          <div className="px-4 py-4 sm:px-6">
            {m.html ? (
              <Frame html={m.html} />
            ) : (
              <pre className="font-sans text-sm whitespace-pre-wrap break-words">
                {m.text}
              </pre>
            )}
          </div>
        </>
      ) : null}
    </article>
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
  message,
  parts,
}: {
  config: string;
  message: string;
  parts: { index: number; name: string; size: number; listed: boolean }[];
}) {
  const listed = parts.filter((p) => p.listed);
  if (listed.length === 0) return null;
  return (
    <ul
      aria-label="Attachments"
      className="flex flex-wrap gap-2 border-b border-line px-4 py-3 sm:px-6"
    >
      {listed.map((p) => (
        <li key={p.index}>
          <a
            href={partURL(config, message, p.index)}
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
