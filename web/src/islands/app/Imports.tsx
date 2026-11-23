import { useEffect, useRef } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { getEmailConfigsByIdMailboxes } from "@app/api/actions/emailConfigs";
import {
  deleteImportsById,
  getImports,
  postImportsByIdCancel,
} from "@app/api/actions/imports";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Import } from "@app/api/types";
import { Button } from "@app/components/Button";
import { Cross } from "@app/components/icons";
import { labelOfMailbox } from "@app/islands/app/mailbox";
import { say } from "@app/islands/app/notices";

/** How often imports are read while one runs. */
const POLL = 2000;

/** How long a finished import says so before it goes. */
const LINGER = 5000;

const messages = (n: number) => (n === 1 ? "1 message" : `${n} messages`);

/**
 * Imports of mail from files, in the bottom-right corner: how far each has got, with a way to
 * cancel, and how it ended. See docs/reading.md.
 */
export function Imports() {
  const client = useQueryClient();
  const imports = useQuery({
    queryKey: qk.imports,
    queryFn: getImports,
    refetchInterval: (q) =>
      (q.state.data ?? []).some((i) => i.state === "importing") ? POLL : false,
  });
  const dismiss = useMutation({
    mutationFn: deleteImportsById,
    onMutate: (id) =>
      client.setQueryData<Import[]>(qk.imports, (old) =>
        old?.filter((i) => i.id !== id),
      ),
  });
  // Those drawn importing: one that finished before it was ever drawn goes without a word.
  const drawn = useRef(new Set<string>());
  const all = imports.data ?? [];
  for (const i of all) if (i.state === "importing") drawn.current.add(i.id);

  // Done or cancelled goes after a moment; failed waits to be read.
  const finished = all
    .filter((i) => i.state === "done" || i.state === "cancelled")
    .map((i) => i.id)
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

  const shown = all.filter(
    (i) =>
      i.state === "importing" ||
      i.state === "failed" ||
      drawn.current.has(i.id),
  );
  if (shown.length === 0) return null;
  return (
    <div className="flex flex-col gap-2">
      {shown.map((i) => (
        <Card key={i.id} item={i} onDismiss={() => dismiss.mutate(i.id)} />
      ))}
    </div>
  );
}

function Card({ item: i, onDismiss }: { item: Import; onDismiss: () => void }) {
  const client = useQueryClient();
  const boxes = useQuery({
    queryKey: qk.mailboxes(i.email_config),
    queryFn: () => getEmailConfigsByIdMailboxes(i.email_config),
  });
  const into = boxes.data?.find((mb) => mb.id === i.mailbox);
  const where = into ? labelOfMailbox(into) : "the folder";
  const cancel = useMutation({
    mutationFn: () => postImportsByIdCancel(i.id),
    onSuccess: () => void client.invalidateQueries({ queryKey: qk.imports }),
    onError: (err) => say(messageOf(err)),
  });
  const gone = i.imported + i.skipped;
  const there =
    i.skipped > 0 ? ` ${messages(i.skipped)} were there already.` : "";

  return (
    <div
      role="status"
      className="flex flex-col gap-2 rounded-lg border border-line bg-bg p-3 text-sm shadow-lg"
    >
      {i.state === "importing" ? (
        <>
          <p className="truncate">
            Importing {i.file} into {where}
          </p>
          <div
            aria-hidden="true"
            className="h-1 overflow-hidden rounded-full bg-fill"
          >
            <div
              className="h-full rounded-full bg-brand transition-[width]"
              style={{ width: `${(100 * gone) / Math.max(i.total, 1)}%` }}
            />
          </div>
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs text-muted tabular-nums">
              {gone} of {i.total}
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
        <div className="flex items-start gap-2">
          <p className="min-w-0 flex-1">
            {i.state === "done" ? (
              <>
                Imported {messages(i.imported)} into {where}.{there}
              </>
            ) : i.state === "cancelled" ? (
              <>
                Cancelled: {messages(i.imported)} imported into {where}.
              </>
            ) : (
              <>
                <span className="text-accent">{i.error} </span>
                {messages(i.imported)} imported into {where}.
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
      )}
    </div>
  );
}
