import { useEffect, useReducer, useRef } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getEmailConfigs,
  getEmailConfigsByIdMailboxes,
} from "@app/api/actions/emailConfigs";
import {
  deleteTransfersById,
  getTransfers,
  postTransfersByIdCancel,
  postTransfersByIdRetry,
} from "@app/api/actions/transfers";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Transfer } from "@app/api/types";
import { Button } from "@app/components/Button";
import { Cross } from "@app/components/icons";
import { labelOf } from "@app/islands/app/emailConfig";
import { labelOfMailbox } from "@app/islands/app/mailbox";
import { say } from "@app/islands/app/notices";

/** How often transfers are read while one moves: how far it has got. */
const POLL = 2000;

/** How long a finished transfer says so before it goes. */
const LINGER = 5000;

/** A transfer this small or this quick finishes without a card: the list shows it went. */
const SMALL = 10;
const QUICK = 2;

const messages = (n: number) => (n === 1 ? "1 message" : `${n} messages`);

/**
 * Moves to another account, in the bottom-right corner: how far each has got, with a way to
 * cancel what is left, and how it ended. See docs/reading.md.
 */
export function Transfers() {
  const client = useQueryClient();
  const transfers = useQuery({
    queryKey: qk.transfers,
    queryFn: getTransfers,
    refetchInterval: (q) =>
      (q.state.data ?? []).some((t) => t.state === "moving") ? POLL : false,
  });
  const dismiss = useMutation({
    mutationFn: deleteTransfersById,
    onMutate: (id) =>
      client.setQueryData<Transfer[]>(qk.transfers, (old) =>
        old?.filter((t) => t.id !== id),
      ),
  });
  // Those drawn once: one that finished before it was ever drawn goes without a word.
  const drawn = useRef(new Set<string>());

  const all = transfers.data ?? [];
  const now = Date.now() / 1000;
  const shows = (t: Transfer) =>
    drawn.current.has(t.id) ||
    t.state === "stopped" ||
    t.state === "cancelled" ||
    (t.state === "moving" && (t.total >= SMALL || now - t.created_at >= QUICK));
  const shown = all.filter(shows);
  for (const t of shown) drawn.current.add(t.id);

  // Done unseen goes now; done or cancelled, seen, after a moment. Stopped waits to be read.
  const finished = all
    .filter((t) => t.state === "done" || t.state === "cancelled")
    .map((t) => t.id)
    .join(" ");
  const { mutate: letGo } = dismiss;
  useEffect(() => {
    const timers = finished
      .split(" ")
      .filter(Boolean)
      .map((id) =>
        setTimeout(() => letGo(id), drawn.current.has(id) ? LINGER : 0),
      );
    return () => timers.forEach(clearTimeout);
  }, [finished, letGo]);

  // A moving one too young to draw is drawn once it is not.
  const young = all.some((t) => t.state === "moving" && !shows(t));
  const [, tick] = useReducer((n: number) => n + 1, 0);
  useEffect(() => {
    if (!young) return;
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [young]);

  if (shown.length === 0) return null;
  return (
    <div className="flex flex-col gap-2">
      {shown.map((t) => (
        <Card key={t.id} transfer={t} onDismiss={() => dismiss.mutate(t.id)} />
      ))}
    </div>
  );
}

function Card({
  transfer: t,
  onDismiss,
}: {
  transfer: Transfer;
  onDismiss: () => void;
}) {
  const client = useQueryClient();
  const configs = useQuery({
    queryKey: qk.emailConfigs,
    queryFn: getEmailConfigs,
  });
  const account = configs.data?.find((c) => c.id === t.target_config);
  const theirs = useQuery({
    queryKey: qk.mailboxes(t.target_config),
    queryFn: () => getEmailConfigsByIdMailboxes(t.target_config),
  });
  const ours = useQuery({
    queryKey: qk.mailboxes(t.email_config),
    queryFn: () => getEmailConfigsByIdMailboxes(t.email_config),
  });
  const folder = theirs.data?.find((mb) => mb.id === t.target);
  const from = ours.data?.find((mb) => mb.id === t.mailbox);
  const where = [account && labelOf(account), folder && labelOfMailbox(folder)]
    .filter(Boolean)
    .join(" › ");
  const left = t.total - t.moved;
  // Its jobs ended or wait again, which decides where the list draws its messages.
  const settled = (now: Transfer) => {
    client.setQueryData<Transfer[]>(qk.transfers, (old) =>
      old?.map((x) => (x.id === now.id ? now : x)),
    );
    void client.invalidateQueries({ queryKey: qk.jobs });
  };
  const cancel = useMutation({
    mutationFn: () => postTransfersByIdCancel(t.id),
    onSuccess: settled,
    onError: (err) => say(messageOf(err)),
  });
  const retry = useMutation({
    mutationFn: () => postTransfersByIdRetry(t.id),
    onSuccess: settled,
    onError: (err) => say(messageOf(err)),
  });

  return (
    <div
      role="status"
      className="flex flex-col gap-2 rounded-lg border border-line bg-bg p-3 text-sm shadow-lg"
    >
      {t.state === "moving" ? (
        <>
          <p className="truncate">Moving to {where || "another account"}</p>
          <div
            aria-hidden="true"
            className="h-1 overflow-hidden rounded-full bg-fill"
          >
            <div
              className="h-full rounded-full bg-brand transition-[width]"
              style={{ width: `${(100 * t.moved) / Math.max(t.total, 1)}%` }}
            />
          </div>
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs text-muted tabular-nums">
              {t.moved} of {t.total}
            </span>
            <Button
              size="bar"
              disabled={cancel.isPending}
              onClick={() => cancel.mutate()}
            >
              Cancel
            </Button>
          </div>
        </>
      ) : (
        <>
          <div className="flex items-start gap-2">
            <p className="min-w-0 flex-1">
              {t.state === "done" ? (
                <>
                  Moved {messages(t.moved)} to {where || "another account"}.
                </>
              ) : (
                <>
                  {t.state === "stopped" ? (
                    <span className="text-accent">{t.error} </span>
                  ) : (
                    "Cancelled: "
                  )}
                  {t.moved} moved, {left} still in{" "}
                  {from ? labelOfMailbox(from) : "the folder"}.
                </>
              )}
            </p>
            <button
              type="button"
              aria-label="Dismiss"
              onClick={onDismiss}
              className="-m-1 shrink-0 rounded p-1 text-faint hover:bg-fill hover:text-fg"
            >
              <Cross />
            </button>
          </div>
          {t.state === "stopped" && (
            <div className="flex justify-end">
              <Button
                size="bar"
                disabled={retry.isPending}
                onClick={() => retry.mutate()}
              >
                Try again
              </Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
