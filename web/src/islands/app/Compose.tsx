import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";

import {
  getEmailConfigs,
  postEmailConfigsByIdSend,
} from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { EmailConfig, Outgoing } from "@app/api/types";
import { Button } from "@app/components/Button";
import { useConfirm } from "@app/components/Confirm";
import { Dialog } from "@app/components/Dialog";
import { Field } from "@app/components/Field";
import { fieldLook, TextField } from "@app/components/TextField";
import { Cross } from "@app/components/icons";
import { size } from "@app/format";
import {
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

/** The compose window, over whatever is open, while something is being written. */
export function Compose() {
  const writing = useWriting();
  if (!writing) return null;
  return <Writer start={writing.draft} key={writing.id} />;
}

function Writer({ start }: { start: Draft }) {
  const confirm = useConfirm();
  const form = useId();
  const [draft, setDraft] = useState(start);
  const [copies, setCopies] = useState(Boolean(start.cc || start.bcc));
  const [problem, setProblem] = useState("");
  const configs = useQuery({
    queryKey: qk.emailConfigs,
    queryFn: getEmailConfigs,
  });
  const config = configs.data?.find((c) => c.id === draft.config);

  const send = useMutation({
    mutationFn: async (d: Draft) =>
      postEmailConfigsByIdSend(d.config, await outgoing(d)),
    onSuccess: () => {
      stopWriting();
      say("Sent.");
    },
  });

  const changed =
    draft.to !== start.to ||
    draft.cc !== start.cc ||
    draft.bcc !== start.bcc ||
    draft.subject !== start.subject ||
    draft.text !== start.text ||
    draft.files.length > 0;

  useEffect(() => {
    if (!changed || send.isSuccess) return;
    const stay = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", stay);
    return () => window.removeEventListener("beforeunload", stay);
  }, [changed, send.isSuccess]);

  const text = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    if (start.reply) text.current?.setSelectionRange(0, 0);
  }, [start.reply]);

  const close = async () => {
    if (send.isPending) return;
    if (
      changed &&
      !(await confirm({
        title: "Discard this message?",
        message: "What is written here is not kept anywhere.",
        confirm: "Discard",
        danger: true,
      }))
    ) {
      return;
    }
    stopWriting();
  };

  const submit = (e?: FormEvent) => {
    e?.preventDefault();
    const total =
      draft.files.reduce((n, f) => n + f.size, 0) +
      (draft.forward?.parts ?? []).reduce((n, p) => n + p.size, 0);
    if (total > LIMIT) {
      setProblem(
        "Attachments come to more than 25 MB, which most mail servers will not take.",
      );
      return;
    }
    setProblem("");
    send.mutate(draft);
  };

  const edit = (patch: Partial<Draft>) => setDraft((d) => ({ ...d, ...patch }));
  const title = draft.reply
    ? "Reply"
    : draft.forward
      ? "Forward"
      : "New message";

  if (config && !config.outgoing) {
    return (
      <Dialog open onClose={stopWriting} title={title}>
        <p className="text-sm text-muted">
          This email config has no outgoing server to send through.{" "}
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

  const error = problem || (send.error ? messageOf(send.error) : "");
  return (
    <Dialog
      open
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
          <label className="mr-auto">
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
          <Button onClick={close} disabled={send.isPending}>
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
              data-autofocus={draft.reply ? undefined : true}
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
            data-autofocus={draft.reply ? true : undefined}
            className={`${fieldLook} min-h-48 resize-y py-2 leading-relaxed`}
          />
        </label>
        <Attached
          draft={draft}
          onForgetPart={(section) =>
            setDraft((d) => ({
              ...d,
              forward: d.forward && {
                ...d.forward,
                parts: d.forward.parts.filter((p) => p.section !== section),
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

/** What goes with it: the forwarded message's attachments, and files from this device. */
function Attached({
  draft,
  onForgetPart,
  onForgetFile,
}: {
  draft: Draft;
  onForgetPart: (section: string) => void;
  onForgetFile: (index: number) => void;
}) {
  const parts = draft.forward?.parts ?? [];
  if (parts.length === 0 && draft.files.length === 0) return null;
  return (
    <ul aria-label="Attachments" className="flex flex-wrap gap-2">
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

/** A draft as the server is asked to send it, files read into base64. */
async function outgoing(d: Draft): Promise<Outgoing> {
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
    forward: d.forward && {
      mailbox: d.forward.mailbox,
      message: d.forward.message,
      parts: d.forward.parts.map((p) => p.section),
    },
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
