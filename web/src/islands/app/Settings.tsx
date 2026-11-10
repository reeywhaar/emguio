import { useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { getAuthMe, postAuthLogout } from "@app/api/actions/auth";
import {
  deleteEmailConfigsById,
  getEmailConfigs,
  getEmailConfigsByIdMailboxes,
  postEmailConfigsByIdTest,
  putEmailConfigsByIdArchive,
} from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { EmailConfig, Mailbox } from "@app/api/types";
import { Button, buttonLook } from "@app/components/Button";
import { Dummy } from "@app/components/Dummy";
import { Select } from "@app/components/Field";
import { ConfigForm } from "@app/islands/app/ConfigForm";
import { describe, draftOf, labelOf } from "@app/islands/app/emailConfig";
import { Link } from "@app/islands/app/Link";
import { depthOf, labelOfMailbox } from "@app/islands/app/mailbox";
import { Delivery, Notifications } from "@app/islands/app/Notifications";
import { Results } from "@app/islands/app/Results";
import { go, paths, type Route } from "@app/islands/app/route";
import { leaveFor } from "@app/leave";
import { forget } from "@app/push";

export function Settings({
  editing,
}: {
  editing: Extract<Route, { page: "settings" }>["editing"];
}) {
  const configs = useQuery({
    queryKey: qk.emailConfigs,
    queryFn: getEmailConfigs,
  });
  // The form last opened stays while it fades out; each opening is a new one.
  const [last, setLast] = useState({ editing, opened: 0 });
  if (editing && editing !== last.editing) {
    setLast({ editing, opened: last.opened + 1 });
  }

  return (
    <main className="mx-auto flex w-full max-w-2xl flex-col gap-6 px-4 py-6">
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
          configs.data.map((c) => <Card key={c.id} config={c} />)
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
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-line bg-bg p-4">
        <span className="text-sm">
          Signed in as{" "}
          {me.data ? (
            <span className="font-medium">{me.data.username}</span>
          ) : (
            "…"
          )}
        </span>
        <Button
          size="bar"
          disabled={signOut.isPending}
          onClick={() => signOut.mutate()}
        >
          Sign out
        </Button>
      </div>
    </section>
  );
}

function Card({ config }: { config: EmailConfig }) {
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

  return (
    <article className="flex flex-col gap-3 rounded-lg border border-line bg-bg p-4">
      <div>
        <h3 className="font-medium">{label}</h3>
        {config.name ? (
          <p className="text-sm text-muted">{config.email}</p>
        ) : null}
      </div>
      <dl className="flex flex-col gap-3 text-sm sm:grid sm:grid-cols-[auto_1fr] sm:gap-x-3 sm:gap-y-2">
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
    </article>
  );
}

/** A label over its value on a phone, and beside it where there is room. */
function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1 sm:col-span-2 sm:grid sm:grid-cols-subgrid sm:items-center">
      <dt className="text-muted">{label}</dt>
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
