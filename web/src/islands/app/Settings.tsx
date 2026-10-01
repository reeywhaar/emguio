import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { getAuthMe, postAuthLogout } from "@app/api/actions/auth";
import {
  deleteEmailConfigsById,
  getEmailConfigs,
  postEmailConfigsByIdTest,
} from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { EmailConfig } from "@app/api/types";
import { Button, buttonLook } from "@app/components/Button";
import { Dummy } from "@app/components/Dummy";
import { ConfigForm } from "@app/islands/app/ConfigForm";
import { describe, draftOf, labelOf } from "@app/islands/app/emailConfig";
import { Link } from "@app/islands/app/Link";
import { Results } from "@app/islands/app/Results";
import { go, paths, type Route } from "@app/islands/app/route";
import { leaveFor } from "@app/leave";

export function Settings({
  editing,
}: {
  editing: Extract<Route, { page: "settings" }>["editing"];
}) {
  const configs = useQuery({
    queryKey: qk.emailConfigs,
    queryFn: getEmailConfigs,
  });

  return (
    <main className="mx-auto flex w-full max-w-2xl flex-col gap-6 px-4 py-6">
      <h1 className="text-xl font-semibold">Settings</h1>
      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between gap-3">
          <h2 className="caps text-muted">Email configs</h2>
          <Link href={paths.newConfig} className={buttonLook("quiet", "bar")}>
            Add email config
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
      <Account />
      {editing ? (
        <ConfigForm
          id={editing.id}
          onClose={() => go(paths.settings)}
          key={editing.id ?? "new"}
        />
      ) : null}
    </main>
  );
}

function Account() {
  const me = useQuery({ queryKey: qk.me, queryFn: getAuthMe });
  const signOut = useMutation({
    mutationFn: postAuthLogout,
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
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
        <dt className="text-muted">Incoming</dt>
        <dd className="break-all">
          {describe(config.incoming, config.incoming.protocol.toUpperCase())}
        </dd>
        <dt className="text-muted">Outgoing</dt>
        <dd className="break-all">
          {config.outgoing ? describe(config.outgoing, "SMTP") : "None"}
        </dd>
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
