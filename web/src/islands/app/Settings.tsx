import { useState, useSyncExternalStore, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { getAuthMe, postAuthLogout } from "@app/api/actions/auth";
import {
  deleteEmailConfigsById,
  getEmailConfigs,
  getEmailConfigsByIdMailboxes,
  postEmailConfigsByIdTest,
  putEmailConfigsByIdArchive,
  putEmailConfigsOrder,
} from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { EmailConfig, Mailbox } from "@app/api/types";
import { Button, buttonLook } from "@app/components/Button";
import { Dummy } from "@app/components/Dummy";
import { Select } from "@app/components/Field";
import { Dialog } from "@app/components/Dialog";
import { ChevronMark, GripMark } from "@app/components/icons";
import { useCarry } from "@app/islands/app/carry";
import { ConfigForm } from "@app/islands/app/ConfigForm";
import {
  describe,
  draftOf,
  labelOf,
  serversOf,
} from "@app/islands/app/emailConfig";
import { Link } from "@app/islands/app/Link";
import { depthOf, labelOfMailbox } from "@app/islands/app/mailbox";
import { say } from "@app/islands/app/notices";
import {
  Delivery,
  Notifications,
  notifiesOf,
} from "@app/islands/app/Notifications";
import { Results } from "@app/islands/app/Results";
import { go, paths, type Route } from "@app/islands/app/route";
import { leaveFor } from "@app/leave";
import { forget } from "@app/push";

export function Settings({
  editing,
}: {
  editing: Extract<Route, { page: "settings" }>["editing"];
}) {
  const client = useQueryClient();
  const configs = useQuery({
    queryKey: qk.emailConfigs,
    queryFn: getEmailConfigs,
  });
  // Shown in the new order at once, and back as it was if the server refuses it.
  const arrange = useMutation({
    mutationFn: putEmailConfigsOrder,
    onMutate: async (ids) => {
      await client.cancelQueries({ queryKey: qk.emailConfigs });
      const before = client.getQueryData<EmailConfig[]>(qk.emailConfigs);
      if (before) {
        client.setQueryData(
          qk.emailConfigs,
          ids.flatMap((id) => before.filter((c) => c.id === id)),
        );
      }
      return before;
    },
    onError: (err, _ids, before) => {
      if (before) client.setQueryData(qk.emailConfigs, before);
      say(messageOf(err));
    },
    onSuccess: (now) => client.setQueryData(qk.emailConfigs, now),
  });
  // The form last opened stays while it fades out; each opening is a new one.
  const [last, setLast] = useState({ editing, opened: 0 });
  if (editing && editing !== last.editing) {
    setLast({ editing, opened: last.opened + 1 });
  }

  return (
    <main className="mx-auto flex w-full max-w-4xl flex-col gap-6 px-4 py-6">
      <h1 className="text-xl font-semibold">Settings</h1>
      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between gap-3">
          <h2 className="caps text-muted">Mail accounts</h2>
          <Link href={paths.newConfig} className={buttonLook("quiet", "bar")}>
            Add mail account
          </Link>
        </div>
        {!configs.data ? (
          <Dummy className="h-32 w-full" />
        ) : configs.data.length === 0 ? (
          <p className="text-sm text-muted">
            None yet. Add one to read its mail here.
          </p>
        ) : (
          <Accounts
            list={configs.data}
            onArrange={(ids) => arrange.mutate(ids)}
          />
        )}
      </section>
      <Notifications />
      <Account />
      <p className="text-sm text-muted">
        A program can do what this page does, through the API:{" "}
        <a href="/docs" className="underline">
          its reference
        </a>
        .
      </p>
      {last.editing ? (
        <ConfigForm
          id={last.editing.id}
          open={editing !== null}
          onClose={() => go(paths.settings)}
          key={last.opened}
        />
      ) : null}
    </main>
  );
}

function Account() {
  const me = useQuery({ queryKey: qk.me, queryFn: getAuthMe });
  const signOut = useMutation({
    // A device signed out of shows nobody's mail.
    mutationFn: async () => {
      await forget();
      await postAuthLogout();
    },
    onSuccess: () => leaveFor("/login"),
  });
  return (
    <section className="flex flex-col gap-3">
      <h2 className="caps text-muted">You</h2>
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-line bg-bg px-4 py-2">
        <span className="text-sm">
          Signed in as{" "}
          {me.data ? (
            <span className="font-medium">{me.data.username}</span>
          ) : (
            "…"
          )}
        </span>
        <Button
          variant="link"
          className="-mr-2 rounded-md px-2 py-1.5"
          disabled={signOut.isPending}
          onClick={() => signOut.mutate()}
        >
          Sign out
        </Button>
      </div>
    </section>
  );
}

/** Wide enough for the accounts and the one open side by side: md. */
const BESIDE = "(min-width: 48rem)";

/** Whether query matches, as the window changes. */
function useMatches(query: string) {
  return useSyncExternalStore(
    (changed) => {
      const list = window.matchMedia(query);
      list.addEventListener("change", changed);
      return () => list.removeEventListener("change", changed);
    },
    () => window.matchMedia(query).matches,
  );
}

/**
 * The mail accounts, dragged into order by their grips. Wide, the one picked is beside them, the
 * first until one is; on a phone, the one picked takes the screen until closed.
 */
function Accounts({
  list,
  onArrange,
}: {
  list: EmailConfig[];
  onArrange: (ids: string[]) => void;
}) {
  const beside = useMatches(BESIDE);
  const [picked, setPicked] = useState<string | null>(null);
  // On a phone the one picked stays while its dialog fades out.
  const [open, setOpen] = useState(false);
  const chosen = list.find((c) => c.id === picked);
  const shown = beside ? (chosen ?? list[0]) : undefined;
  // The account being carried, and the one it would go before or after.
  const [carrying, setCarrying] = useState<string | null>(null);
  const [onto, setOnto] = useState<string | null>(null);
  const ids = list.map((c) => c.id);
  const down =
    carrying && onto ? ids.indexOf(carrying) < ids.indexOf(onto) : false;
  const drop = () => {
    setCarrying(null);
    setOnto(null);
    if (!carrying || !onto) return;
    const next = ids.filter((id) => id !== carrying);
    next.splice(ids.indexOf(onto), 0, carrying);
    onArrange(next);
  };

  const rows = (
    <ul
      aria-label="Mail accounts"
      className="flex flex-col divide-y divide-line overflow-hidden rounded-lg border border-line bg-bg"
    >
      {list.map((c) => (
        <li
          key={c.id}
          className={`relative ${c.id === carrying ? "opacity-40" : ""}`}
        >
          <Entry
            config={c}
            current={c.id === shown?.id}
            beside={beside}
            arrangeable={list.length > 1}
            onPick={() => {
              setPicked(c.id);
              setOpen(true);
            }}
            onStart={() => setCarrying(c.id)}
            onOver={(id) => setOnto(id === c.id ? null : id)}
            onDrop={drop}
          />
          {/* Absolute, so it takes no room and the rows do not shift under the pointer. */}
          {onto === c.id ? (
            <span
              aria-hidden="true"
              className={`pointer-events-none absolute inset-x-0 h-0.5 bg-fg ${down ? "bottom-0" : "top-0"}`}
            />
          ) : null}
        </li>
      ))}
    </ul>
  );
  if (!beside) {
    return (
      <>
        {rows}
        <Dialog
          open={open && chosen !== undefined}
          onClose={() => setOpen(false)}
          title={chosen ? labelOf(chosen) : ""}
        >
          {chosen ? <Details key={chosen.id} config={chosen} /> : null}
        </Dialog>
      </>
    );
  }
  return (
    <div className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)] items-start gap-3">
      {rows}
      {shown ? <Details key={shown.id} config={shown} titled /> : null}
    </div>
  );
}

/** An account in the list: picked by a press, carried into its place by its grip. */
function Entry({
  config,
  current,
  beside,
  arrangeable,
  onPick,
  onStart,
  onOver,
  onDrop,
}: {
  config: EmailConfig;
  /** Shown beside the list. */
  current: boolean;
  beside: boolean;
  /** There are others to put it among. */
  arrangeable: boolean;
  onPick: () => void;
  onStart: () => void;
  /** Carried over the account with this id, or over none. */
  onOver: (id: string | null) => void;
  onDrop: () => void;
}) {
  const boxes = useQuery({
    queryKey: qk.mailboxes(config.id),
    queryFn: () => getEmailConfigsByIdMailboxes(config.id),
  });
  const carry = useCarry({
    find: "[data-account]",
    grip: "[data-grip]",
    enabled: arrangeable,
    onStart,
    onOver: (el) => onOver(el?.dataset.account ?? null),
    onDrop,
  });
  // No selecting, and no long-press preview, of a row a finger may be picking up.
  const still = arrangeable ? "select-none [-webkit-touch-callout:none]" : "";
  const look = current ? "bg-shade" : "hover:bg-shade/50";
  return (
    <button
      type="button"
      data-account={config.id}
      ref={carry.hold}
      draggable={false}
      onPointerDown={carry.press}
      onPointerMove={carry.move}
      onPointerUp={carry.release}
      onPointerCancel={carry.cancel}
      onContextMenu={carry.menu}
      aria-current={current ? "true" : undefined}
      aria-haspopup={beside ? undefined : "dialog"}
      className={`flex w-full items-center gap-2 py-3 pr-4 text-left text-sm font-medium ${arrangeable ? "pl-2" : "pl-4"} ${still} ${look}`}
      onClick={() => {
        if (carry.spent.current) {
          carry.spent.current = false;
          return;
        }
        onPick();
      }}
    >
      {arrangeable ? (
        <span data-grip className="shrink-0 cursor-grab touch-none text-faint">
          <GripMark />
        </span>
      ) : null}
      <span className="min-w-0 flex-1">
        <span className="block truncate">{labelOf(config)}</span>
        {config.name ? (
          <span className="block truncate text-xs font-normal text-muted">
            {config.email}
          </span>
        ) : null}
        <span className="block text-[0.625rem] leading-4 font-normal text-faint">
          {serversOf(config)} · {notifiesOf(config, boxes.data)}
        </span>
      </span>
      {beside ? null : (
        <span className="shrink-0 -rotate-90 text-muted">
          <ChevronMark />
        </span>
      )}
    </button>
  );
}

/** An account's servers and choices, and what can be done to it; titled when not in a dialog. */
function Details({
  config,
  titled = false,
}: {
  config: EmailConfig;
  titled?: boolean;
}) {
  const client = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const test = useMutation({
    mutationFn: () => postEmailConfigsByIdTest(config.id, draftOf(config)),
  });
  const remove = useMutation({
    mutationFn: () => deleteEmailConfigsById(config.id),
    onSuccess: () => client.invalidateQueries({ queryKey: qk.emailConfigs }),
  });
  const label = labelOf(config);

  const body = (
    <>
      <dl className="flex flex-col gap-4 text-sm">
        <Row label="Incoming">
          {describe(config.incoming, config.incoming.protocol.toUpperCase())}
        </Row>
        <Row label="Outgoing">
          {config.outgoing ? describe(config.outgoing, "SMTP") : "None"}
        </Row>
        <Row label="Archive to">
          <ArchiveTo config={config} />
        </Row>
        <Row label="Notify">
          <Delivery config={config} />
        </Row>
      </dl>
      {test.data ? <Results result={test.data} /> : null}
      {test.error ? (
        <p className="text-sm text-accent">{messageOf(test.error)}</p>
      ) : null}
      {remove.error ? (
        <p className="text-sm text-accent">{messageOf(remove.error)}</p>
      ) : null}
      {confirming ? (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm">Delete {label}?</span>
          <Button
            size="bar"
            variant="danger"
            disabled={remove.isPending}
            onClick={() => remove.mutate()}
          >
            Delete
          </Button>
          <Button size="bar" onClick={() => setConfirming(false)}>
            Keep it
          </Button>
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          <Link
            href={paths.editConfig(config.id)}
            className={buttonLook("quiet", "bar")}
          >
            Edit
          </Link>
          <Button
            size="bar"
            disabled={test.isPending}
            onClick={() => test.mutate()}
          >
            {test.isPending ? "Testing" : "Test connection"}
          </Button>
          <Button
            size="bar"
            variant="danger"
            onClick={() => setConfirming(true)}
          >
            Delete…
          </Button>
        </div>
      )}
    </>
  );
  if (!titled) {
    return (
      <div className="flex flex-col gap-4">
        {config.name ? (
          <p className="-mt-2 text-sm text-muted wrap-anywhere">
            {config.email}
          </p>
        ) : null}
        {body}
      </div>
    );
  }
  return (
    <article className="flex flex-col gap-4 rounded-lg border border-line bg-bg p-4">
      <div className="min-w-0">
        <h3 className="font-medium wrap-anywhere">{label}</h3>
        {config.name ? (
          <p className="text-sm text-muted wrap-anywhere">{config.email}</p>
        ) : null}
      </div>
      {body}
    </article>
  );
}

/** A label over its text or control: beside it, a control never lines up with plain text. */
function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-xs text-muted">{label}</dt>
      <dd className="min-w-0 wrap-anywhere">{children}</dd>
    </div>
  );
}

/** Folders mail can be archived to: the server's own for it, and the user's. */
const archivable = new Set<Mailbox["special_use"]>(["", "archive", "all"]);

/**
 * Where archiving moves this config's mail: the folder the server names, or one chosen here, for
 * a server that names none or names another. Saved as it is chosen. See docs/reading.md.
 */
function ArchiveTo({ config }: { config: EmailConfig }) {
  const client = useQueryClient();
  const boxes = useQuery({
    queryKey: qk.mailboxes(config.id),
    queryFn: () => getEmailConfigsByIdMailboxes(config.id),
  });
  const choose = useMutation({
    mutationFn: (mailbox: string) =>
      putEmailConfigsByIdArchive(config.id, mailbox),
    onSuccess: (saved) =>
      client.setQueryData<EmailConfig[]>(qk.emailConfigs, (old) =>
        old?.map((c) => (c.id === saved.id ? saved : c)),
      ),
  });
  if (!boxes.data) return <Dummy className="h-8 w-48" />;
  const usable = boxes.data.filter((mb) => mb.selectable);
  const server =
    usable.find((mb) => mb.special_use === "archive") ??
    usable.find((mb) => mb.special_use === "all");
  return (
    <div className="flex flex-col gap-1">
      <Select
        size="bar"
        aria-label="Archive to"
        className="max-w-full sm:max-w-72"
        value={
          choose.isPending ? choose.variables : (config.archive_mailbox ?? "")
        }
        disabled={choose.isPending}
        onChange={(e) => choose.mutate(e.target.value)}
      >
        <option value="">
          {server
            ? `Automatic: ${labelOfMailbox(server)}`
            : "Automatic: the server names none"}
        </option>
        {usable
          .filter((mb) => archivable.has(mb.special_use))
          .map((mb) => (
            <option key={mb.id} value={mb.id}>
              {" ".repeat(depthOf(mb))}
              {labelOfMailbox(mb)}
            </option>
          ))}
      </Select>
      {choose.error ? (
        <p className="text-sm text-accent">{messageOf(choose.error)}</p>
      ) : null}
    </div>
  );
}
