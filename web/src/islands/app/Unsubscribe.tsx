import { useMutation } from "@tanstack/react-query";

import { postEmailConfigsByIdMailboxesByMailboxMessagesByMessageUnsubscribe } from "@app/api/actions/emailConfigs";
import { messageOf } from "@app/api/transport";
import type { ReadMessage } from "@app/api/types";
import { Button, buttonLook } from "@app/components/Button";

/** The mark of mail that says how to unsubscribe from it. */
export function NewsletterTag() {
  return (
    <span className="shrink-0 self-center rounded border border-line px-1 text-[0.6875rem] leading-4 text-muted">
      Newsletter
    </span>
  );
}

/**
 * Stopping a newsletter's mail: here, at a press, where its sender takes one request for it, and
 * otherwise on the sender's own page, in a new tab. See docs/reading.md.
 */
export function Unsubscribe({
  config,
  mailbox,
  message,
  how,
}: {
  config: string;
  mailbox: string;
  message: string;
  how: NonNullable<ReadMessage["unsubscribe"]>;
}) {
  const stop = useMutation({
    mutationFn: () =>
      postEmailConfigsByIdMailboxesByMailboxMessagesByMessageUnsubscribe(
        config,
        mailbox,
        message,
      ),
  });
  const page = (label: string) => (
    <a
      href={how.link}
      target="_blank"
      rel="noreferrer noopener"
      className={buttonLook("link")}
    >
      {label}
    </a>
  );
  if (!how.one_click) return page("Unsubscribe");
  if (stop.isSuccess) {
    return <span className="text-sm text-muted">Unsubscribed</span>;
  }
  return (
    <>
      <Button
        variant="link"
        disabled={stop.isPending}
        onClick={() => stop.mutate()}
      >
        {stop.isPending ? "Unsubscribing" : "Unsubscribe"}
      </Button>
      {stop.error ? (
        <span className="text-sm text-accent">
          {messageOf(stop.error)} {page("Open their page")}
        </span>
      ) : null}
    </>
  );
}
