import {
  useMutationState,
  type InfiniteData,
  type QueryClient,
} from "@tanstack/react-query";

import { useCached } from "@app/api/cached";
import { mk, qk } from "@app/api/keys";
import type {
  Job,
  JobDraft,
  Mailbox,
  Message,
  MessagePage,
  ReadMessage,
} from "@app/api/types";

/**
 * What somebody asked to be done and is not done yet, drawn over what the server last said
 * rather than written into it: the jobs the server has taken and not yet done, and the ones
 * still on their way to it.
 *
 * The cache holds only what the server has said or done. A refusal leaves nothing to undo: the
 * job stops being pending and the screen is the server's again, whatever was asked after it.
 * A list read back while jobs wait is drawn with them still applied. And a page opened after
 * they were asked — another tab, a reload — draws them too, from the server's list.
 */
export type Pending = JobDraft & {
  /** Names a job on its way, so it is drawn once when the server's list has it too. */
  ref?: string;
  /** What the message is called, for the sentence should the job fail. */
  label?: string;
};

/** Jobs on their way that the server has taken, by ref: drawn from its list from then on. */
export const accepted = new Map<string, string>();

/** What each job is about, by id, for the sentence that says it failed. */
export const labels = new Map<string, string>();

let refs = 0;
export const nextRef = () => String(++refs);

/** A pending job as the server is asked it, without what names it here. */
export const draftOf = (p: Pending): JobDraft => ({
  email_config: p.email_config,
  mailbox: p.mailbox,
  message: p.message,
  kind: p.kind,
  value: p.value,
  target: p.target,
  seen: p.seen,
});

/** One config's pending jobs, in the order asked. */
export function usePending(config: string): Pending[] {
  const taken = useCached<Job[]>(qk.jobs) ?? [];
  const sending = useMutationState({
    filters: { mutationKey: mk.actionsOf(config), status: "pending" },
    select: (m) => m.state.variables as Pending[],
  }).flat();
  return [
    ...taken.filter((j) => j.email_config === config && !j.error),
    ...sending.filter((p) => !(p.ref && accepted.has(p.ref))),
  ];
}

/** What a seen or flagged job changes of its message. */
function change(p: JobDraft): Partial<Message> {
  if (p.kind === "seen") return { seen: p.value };
  if (p.kind === "flagged") return { flagged: p.value };
  return {};
}

/** A message as it is once its pending jobs are done. */
export function withPending<T extends Pick<Message, "id" | "seen" | "flagged">>(
  m: T,
  mailbox: string,
  pending: Pending[],
): T {
  let out = m;
  for (const p of pending) {
    if (p.mailbox === mailbox && p.message === m.id) {
      out = { ...out, ...change(p) };
    }
  }
  return out;
}

const removes = (p: JobDraft) => p.kind === "move" || p.kind === "delete";

/** A mailbox's list as it is once its pending jobs are done: moved ones gone, flags changed. */
export function listWithPending(
  msgs: Message[],
  mailbox: string,
  pending: Pending[],
): Message[] {
  const gone = new Set(
    pending
      .filter((p) => removes(p) && p.mailbox === mailbox)
      .map((p) => p.message),
  );
  return msgs
    .filter((m) => !gone.has(m.id))
    .map((m) => withPending(m, mailbox, pending));
}

/** What a job does to the mailboxes' counts: each mailbox, and by how much. */
export function effects(p: JobDraft): [string, number, number][] {
  const unseen = p.seen ? 0 : 1;
  if (removes(p)) {
    const out: [string, number, number][] = [[p.mailbox, -1, -unseen]];
    if (p.target) out.push([p.target, 1, unseen]);
    return out;
  }
  if (p.kind !== "seen" || p.value === p.seen) return [];
  return [[p.mailbox, 0, p.value ? -1 : 1]];
}

/** Mailboxes moved by the given effects. */
export function counted(
  boxes: Mailbox[],
  moves: [string, number, number][],
): Mailbox[] {
  if (moves.length === 0) return boxes;
  return boxes.map((mb) => {
    let messages = mb.messages;
    let unseen = mb.unseen;
    for (const [id, dm, du] of moves) {
      if (id !== mb.id) continue;
      messages += dm;
      unseen += du;
    }
    return messages === mb.messages && unseen === mb.unseen
      ? mb
      : { ...mb, messages: Math.max(0, messages), unseen: Math.max(0, unseen) };
  });
}

/** The mailboxes' counts as they are once the pending jobs are done. */
export function countsWithPending(
  boxes: Mailbox[],
  pending: Pending[],
): Mailbox[] {
  return counted(boxes, pending.flatMap(effects));
}

/**
 * Writes a job the server has done into what the server last said, in the step that stops it
 * being drawn as pending, so the screen does not show it undone in between.
 */
export function commit(client: QueryClient, j: JobDraft) {
  // The folder's list and every search of it that shows the message.
  const rows = (edit: (msgs: Message[]) => Message[]) =>
    client.setQueriesData<InfiniteData<MessagePage>>(
      { queryKey: qk.folder(j.email_config, j.mailbox) },
      (old) =>
        old && {
          ...old,
          pages: old.pages.map((p) => ({ ...p, messages: edit(p.messages) })),
        },
    );
  if (removes(j)) {
    rows((msgs) => msgs.filter((x) => x.id !== j.message));
    if (j.target) {
      client.invalidateQueries({
        queryKey: qk.folder(j.email_config, j.target),
      });
    }
  } else {
    const reading = qk.message(j.email_config, j.mailbox, j.message);
    // A read underway may answer with the message as it was before the job: it is read again.
    if (client.getQueryState(reading)?.fetchStatus === "fetching") {
      void client
        .cancelQueries({ queryKey: reading })
        .then(() => client.invalidateQueries({ queryKey: reading }));
    } else {
      client.setQueryData<ReadMessage>(
        reading,
        (old) => old && { ...old, ...change(j) },
      );
    }
    rows((msgs) =>
      msgs.map((x) => (x.id === j.message ? { ...x, ...change(j) } : x)),
    );
  }
  client.setQueryData<Mailbox[]>(
    qk.mailboxes(j.email_config),
    (old) => old && counted(old, effects(j)),
  );
}

/** The sentence for jobs that were not done: one by what it is called, more by how many. */
export function failure(
  jobs: JobDraft[],
  label: string | undefined,
  why: string,
) {
  const j = jobs[0]!;
  const many = jobs.length > 1;
  const was = many ? "were" : "was";
  const not = {
    seen: j.value ? "marked read" : "marked unread",
    flagged: j.value ? "starred" : "unstarred",
    move: "moved",
    delete: "deleted",
  }[j.kind];
  const what = many
    ? `${jobs.length} messages`
    : label
      ? `“${label}”`
      : "A message";
  return `${what} ${was} not ${not}: ${why}`;
}
