import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { useMutation, useQuery } from "@tanstack/react-query";

import {
  deleteEmailConfigsByIdDraftsByDraft,
  getEmailConfigs,
  postEmailConfigsByIdDrafts,
  postEmailConfigsByIdSend,
  putEmailConfigsByIdDraftsByDraft,
} from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { ApiError, messageOf } from "@app/api/transport";
import type {
  ClosedDraft,
  EmailConfig,
  KeptDraft,
  Outgoing,
} from "@app/api/types";
import { Button } from "@app/components/Button";
import { useConfirm } from "@app/components/Confirm";
import { Dialog } from "@app/components/Dialog";
import { Field } from "@app/components/Field";
import { fieldLook, TextField } from "@app/components/TextField";
import { Cross } from "@app/components/icons";
import { size } from "@app/format";
import { useAsk } from "@app/islands/app/ask";
import {
  differs,
  settled,
  stopWriting,
  typed,
  useWriting,
  type Draft,
} from "@app/islands/app/drafts";
import { Link } from "@app/islands/app/Link";
import { say } from "@app/islands/app/notices";
import { paths } from "@app/islands/app/route";

/** What most mail servers take, attachments and all; the server says the same. */
const LIMIT = 25 * 1024 * 1024;

/** How long writing pauses before what is written is kept in emguio. */
const AUTOSAVE = 2000;

/** How closing went: put in Drafts or kept to be, nowhere to keep it, or emguio not reached. */
type Ending = ClosedDraft | { nowhere: true } | { failed: true };

/** The compose window, over whatever is open, while something is being written. */
export function Compose() {
  const writing = useWriting();
  // The last draft stays while its window fades out.
  const [last, setLast] = useState(writing);
  if (writing && writing !== last) setLast(writing);
  if (!last) return null;
  return <Writer start={last.draft} open={writing !== null} key={last.id} />;
}

/**
 * The window a message is written in. What is written is kept in Drafts as it is written, each
 * save in place of the last; see docs/sending.md.
 */
function Writer({ start, open }: { start: Draft; open: boolean }) {
  const confirm = useConfirm();
  const ask = useAsk(start.config);
  const form = useId();
  const [draft, setDraft] = useState(start);
  const [copies, setCopies] = useState(Boolean(start.cc || start.bcc));
  const [problem, setProblem] = useState("");
  const configs = useQuery({
    queryKey: qk.emailConfigs,
    queryFn: getEmailConfigs,
  });
  const config = configs.data?.find((c) => c.id === draft.config);

  // What is written, for saves that finish after the render they started in.
  const latest = useRef(draft);
  latest.current = draft;
  // As emguio keeps it, or as it was opened while nothing has been kept.
  const kept = useRef(start);
  // One at a time, so each knows the draft the last one made.
  const saves = useRef<Promise<unknown>>(Promise.resolve());
  // False once the account turns out to have nowhere to keep drafts.
  const keeps = useRef(true);
  // Set while sending or discarding, when another save would be one too many.
  const stopped = useRef(false);
  const closing = useRef(false);
  const [saving, setSaving] = useState(false);
  const [unsaved, setUnsaved] = useState("");

  /**
   * Keeps what is written in emguio, once any save on its way is done; closing, has it written
   * to Drafts now. Says how a closing went; null for nothing to keep.
   */
  const keep = useCallback((close: boolean) => {
    const next = saves.current.then(async (): Promise<Ending | null> => {
      const now = latest.current;
      const untouched = !differs(now, kept.current);
      if (stopped.current || (untouched && (!close || !now.id))) return null;
      if (!keeps.current) return { nowhere: true };
      setSaving(true);
      try {
        const body = await outgoing(now, close);
        if (close) {
          return now.id
            ? await putEmailConfigsByIdDraftsByDraft<ClosedDraft>(
                now.config,
                now.id,
                body,
              )
            : await postEmailConfigsByIdDrafts<ClosedDraft>(now.config, body);
        }
        const res = now.id
          ? await putEmailConfigsByIdDraftsByDraft<KeptDraft>(
              now.config,
              now.id,
              body,
            )
          : await postEmailConfigsByIdDrafts<KeptDraft>(now.config, body);
        kept.current = settled(now, now, res);
        latest.current = settled(latest.current, now, res);
        setDraft((d) => settled(d, now, res));
        setUnsaved(res.problem && `Not in Drafts yet. ${res.problem}`);
        return null;
      } catch (err) {
        if (err instanceof ApiError && err.code === "conflict") {
          keeps.current = false;
          return { nowhere: true };
        }
        setUnsaved(`Not saved. ${messageOf(err)}`);
        return { failed: true };
      } finally {
        setSaving(false);
      }
    });
    saves.current = next;
    return next;
  }, []);

  const changed = differs(draft, kept.current);
  useEffect(() => {
    if (!open || !changed) return;
    const timer = setTimeout(() => keep(false), AUTOSAVE);
    return () => clearTimeout(timer);
  }, [open, changed, draft, keep]);

  const send = useMutation({
    mutationFn: async () => {
      stopped.current = true;
      await saves.current;
      const d = latest.current;
      return postEmailConfigsByIdSend(d.config, await outgoing(d, false));
    },
    onSuccess: () => {
      stopWriting();
      say("Sent.");
    },
    onError: () => {
      stopped.current = false;
    },
  });

  useEffect(() => {
    if (!open || !(changed || saving) || send.isSuccess) return;
    const stay = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", stay);
    return () => window.removeEventListener("beforeunload", stay);
  }, [open, changed, saving, send.isSuccess]);

  const text = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    if (start.reply) text.current?.setSelectionRange(0, 0);
  }, [start.reply]);

  // Closing has what is written put in Drafts, and asks only when it cannot be kept at all.
  const close = async () => {
    if (send.isPending || closing.current) return;
    closing.current = true;
    try {
      const end = await keep(true);
      if (!end || "closed" in end) {
        stopWriting();
        if (end?.closed) say("Saved to Drafts.");
        if (end && !end.closed) {
          say(
            `Saved. It goes in Drafts once the mail server takes it: ${end.problem ?? ""}`,
          );
        }
        return;
      }
      const has = "failed" in end && latest.current.id !== null;
      if (
        await confirm({
          title: has ? "Close without saving?" : "Discard this message?",
          message: has
            ? "What was written since it was last saved is lost."
            : "What is written here is not kept anywhere.",
          confirm: has ? "Close" : "Discard",
          danger: true,
        })
      ) {
        if ("nowhere" in end) await forget();
        stopWriting();
      }
    } finally {
      closing.current = false;
    }
  };

  /** Discards what emguio keeps, and has the mail server's copy deleted. */
  const forget = async () => {
    stopped.current = true;
    await saves.current;
    let copy = latest.current.id ? null : start.draft;
    const id = latest.current.id;
    if (id) {
      try {
        copy = (await deleteEmailConfigsByIdDraftsByDraft(start.config, id))
          .kept;
      } catch (err) {
        say(`The draft was not discarded: ${messageOf(err)}`);
      }
    }
    if (copy) {
      ask([
        {
          email_config: start.config,
          mailbox: copy.mailbox,
          message: copy.message,
          kind: "delete",
          value: false,
          target: "",
          seen: true,
          label: latest.current.subject || "(no subject)",
        },
      ]);
    }
  };

  const discard = async () => {
    if (send.isPending || closing.current) return;
    const has = latest.current.id !== null || start.draft !== null || saving;
    if (
      (has || differs(latest.current, start)) &&
      !(await confirm({
        title: has ? "Discard this draft?" : "Discard this message?",
        message: has
          ? "It is deleted from Drafts."
          : "What is written here is not kept anywhere.",
        confirm: "Discard",
        danger: true,
      }))
    ) {
      return;
    }
    await forget();
    stopWriting();
  };

  const submit = (e?: FormEvent) => {
    e?.preventDefault();
    const total =
      draft.files.reduce((n, f) => n + f.size, 0) +
      draft.held.reduce((n, h) => n + h.size, 0) +
      (draft.carry?.parts ?? []).reduce((n, p) => n + p.size, 0);
    if (total > LIMIT) {
      setProblem(
        "Attachments come to more than 25 MB, which most mail servers will not take.",
      );
      return;
    }
    setProblem("");
    send.mutate();
  };

  const edit = (patch: Partial<Draft>) => setDraft((d) => ({ ...d, ...patch }));
  const title = start.draft
    ? "Draft"
    : start.reply
      ? "Reply"
      : start.carry
        ? "Forward"
        : "New message";

  if (config && !config.outgoing) {
    return (
      <Dialog open={open} onClose={stopWriting} title={title}>
        <p className="text-sm text-muted">
          This mail account has no outgoing server to send through.{" "}
          <Link
            href={paths.editConfig(config.id)}
            className="underline"
            onClick={stopWriting}
          >
            Add one in its settings
          </Link>
        </p>
      </Dialog>
    );
  }

  const error = problem || (send.error ? messageOf(send.error) : "") || unsaved;
  return (
    <Dialog
      open={open}
      wide
      onClose={close}
      title={title}
      footer={
        <>
          {error ? (
            <p role="alert" className="w-full text-sm text-accent">
              {error}
            </p>
          ) : null}
          <label>
            <span className="sr-only">Attach files</span>
            <input
              type="file"
              multiple
              className="peer sr-only"
              onChange={(e) => {
                const picked = [...(e.target.files ?? [])];
                e.target.value = "";
                setDraft((d) => ({ ...d, files: [...d.files, ...picked] }));
              }}
            />
            <span
              aria-hidden="true"
              className="inline-flex min-h-10 cursor-pointer items-center rounded-md border border-line bg-bg px-3 py-1.5 text-sm hover:bg-fill peer-focus-visible:border-brand"
            >
              Attach files
            </span>
          </label>
          <span aria-live="polite" className="mr-auto text-xs text-faint">
            {saving ? "Saving…" : draft.id && !changed ? "Saved" : ""}
          </span>
          <Button onClick={discard} disabled={send.isPending}>
            Discard
          </Button>
          <Button
            type="submit"
            form={form}
            variant="solid"
            disabled={send.isPending || !config}
          >
            {send.isPending ? "Sending" : "Send"}
          </Button>
        </>
      }
    >
      <form
        id={form}
        className="flex flex-col gap-3"
        onSubmit={submit}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) submit();
        }}
      >
        {config ? <From config={config} /> : null}
        <div className="flex items-end gap-2">
          <Field label="To" className="min-w-0 flex-1">
            <TextField
              autoComplete="off"
              spellCheck={false}
              value={draft.to}
              onChange={(e) => edit({ to: e.target.value })}
              data-autofocus={start.reply || start.draft ? undefined : true}
            />
          </Field>
          {copies ? null : (
            <Button onClick={() => setCopies(true)}>Cc, Bcc</Button>
          )}
        </div>
        {copies ? (
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Cc">
              <TextField
                autoComplete="off"
                spellCheck={false}
                value={draft.cc}
                onChange={(e) => edit({ cc: e.target.value })}
              />
            </Field>
            <Field label="Bcc">
              <TextField
                autoComplete="off"
                spellCheck={false}
                value={draft.bcc}
                onChange={(e) => edit({ bcc: e.target.value })}
              />
            </Field>
          </div>
        ) : null}
        <Field label="Subject">
          <TextField
            value={draft.subject}
            onChange={(e) => edit({ subject: e.target.value })}
          />
        </Field>
        <label className="flex flex-col gap-1">
          <span className="sr-only">Message</span>
          <textarea
            ref={text}
            rows={14}
            value={draft.text}
            onChange={(e) => edit({ text: e.target.value })}
            data-autofocus={start.reply || start.draft ? true : undefined}
            className={`${fieldLook} min-h-48 resize-y py-2 leading-relaxed`}
          />
        </label>
        <Attached
          draft={draft}
          onForgetHeld={(id) =>
            setDraft((d) => ({ ...d, held: d.held.filter((h) => h.id !== id) }))
          }
          onForgetPart={(section) =>
            setDraft((d) => ({
              ...d,
              carry: d.carry && {
                ...d.carry,
                parts: d.carry.parts.filter((p) => p.section !== section),
              },
            }))
          }
          onForgetFile={(i) =>
            setDraft((d) => ({
              ...d,
              files: d.files.filter((_, j) => j !== i),
            }))
          }
        />
      </form>
    </Dialog>
  );
}

/** Who it is from: the config's address, under the name it sends with. */
function From({ config }: { config: EmailConfig }) {
  return (
    <p className="text-sm text-muted">
      From {typed({ name: config.sender_name, email: config.email })}
    </p>
  );
}

/**
 * What goes with it: what emguio holds, what it carries from a message on the mail server, and
 * files from this device.
 */
function Attached({
  draft,
  onForgetHeld,
  onForgetPart,
  onForgetFile,
}: {
  draft: Draft;
  onForgetHeld: (id: string) => void;
  onForgetPart: (section: string) => void;
  onForgetFile: (index: number) => void;
}) {
  const parts = draft.carry?.parts ?? [];
  if (draft.held.length + parts.length + draft.files.length === 0) return null;
  return (
    <ul aria-label="Attachments" className="flex flex-wrap gap-2">
      {draft.held.map((h) => (
        <Chip
          key={`held ${h.id}`}
          name={h.name}
          bytes={h.size}
          onForget={() => onForgetHeld(h.id)}
        />
      ))}
      {parts.map((p) => (
        <Chip
          key={`part ${p.section}`}
          name={p.name}
          bytes={p.size}
          onForget={() => onForgetPart(p.section)}
        />
      ))}
      {draft.files.map((f, i) => (
        <Chip
          key={`file ${i} ${f.name}`}
          name={f.name}
          bytes={f.size}
          onForget={() => onForgetFile(i)}
        />
      ))}
    </ul>
  );
}

function Chip({
  name,
  bytes,
  onForget,
}: {
  name: string;
  bytes: number;
  onForget: () => void;
}) {
  return (
    <li className="flex max-w-full items-center gap-2 rounded-md border border-line py-1 pr-1 pl-2.5 text-sm">
      <span className="min-w-0 truncate">{name}</span>
      <span className="shrink-0 text-xs text-muted">{size(bytes)}</span>
      <button
        type="button"
        aria-label={`Remove ${name}`}
        onClick={onForget}
        className="flex size-6 shrink-0 items-center justify-center rounded text-muted hover:bg-fill hover:text-fg"
      >
        <Cross />
      </button>
    </li>
  );
}

/** A draft as the server is asked to send or keep it, files read into base64. */
async function outgoing(d: Draft, close: boolean): Promise<Outgoing> {
  return {
    to: d.to,
    cc: d.cc,
    bcc: d.bcc,
    subject: d.subject,
    text: d.text,
    attachments: await Promise.all(
      d.files.map(async (f) => ({
        name: f.name,
        type: f.type || "application/octet-stream",
        data: await base64(f),
      })),
    ),
    reply: d.reply,
    carry: d.carry && {
      mailbox: d.carry.mailbox,
      message: d.carry.message,
      parts: d.carry.parts.map((p) => p.section),
    },
    draft: d.draft,
    draft_id: d.id,
    parts: d.held.map((h) => h.id),
    close,
  };
}

function base64(f: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.addEventListener("load", () => {
      const url = String(reader.result);
      resolve(url.slice(url.indexOf(",") + 1));
    });
    reader.addEventListener("error", () => reject(reader.error));
    reader.readAsDataURL(f);
  });
}
