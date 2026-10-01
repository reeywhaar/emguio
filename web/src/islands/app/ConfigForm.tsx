import { useId, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getEmailConfigs,
  postEmailConfigs,
  postEmailConfigsAutoconfig,
  postEmailConfigsByIdTest,
  postEmailConfigsTest,
  putEmailConfigsById,
} from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type {
  EmailConfig,
  EmailConfigDraft,
  Protocol,
  Security,
  Settings,
} from "@app/api/types";
import { Button } from "@app/components/Button";
import { Dialog } from "@app/components/Dialog";
import { Dummy } from "@app/components/Dummy";
import { Field, Select } from "@app/components/Field";
import { TextField } from "@app/components/TextField";
import { labelOf } from "@app/islands/app/emailConfig";
import { Results } from "@app/islands/app/Results";

/** The ports each kind of server usually answers on, for each way of securing it. */
const ports = {
  incoming: { implicit: 993, starttls: 143 },
  outgoing: { implicit: 465, starttls: 587 },
} as const;

/** The form's own state: ports as typed, and the outgoing server as two switches. */
type Draft = {
  name: string;
  email: string;
  sender_name: string;
  incoming: {
    protocol: Protocol;
    host: string;
    port: string;
    tls: Security;
    username: string;
    password: string;
  };
  outgoing: {
    on: boolean;
    host: string;
    port: string;
    tls: Security;
    /** Signs in with the incoming username and password. */
    shared: boolean;
    username: string;
    password: string;
  };
};

function blank(): Draft {
  return {
    name: "",
    email: "",
    sender_name: "",
    incoming: {
      protocol: "imap",
      host: "",
      port: String(ports.incoming.implicit),
      tls: "implicit",
      username: "",
      password: "",
    },
    outgoing: {
      on: false,
      host: "",
      port: String(ports.outgoing.implicit),
      tls: "implicit",
      shared: true,
      username: "",
      password: "",
    },
  };
}

function fromConfig(c: EmailConfig): Draft {
  const empty = blank();
  return {
    name: c.name,
    email: c.email,
    sender_name: c.sender_name,
    incoming: { ...c.incoming, port: String(c.incoming.port), password: "" },
    outgoing: c.outgoing
      ? {
          on: true,
          host: c.outgoing.host,
          port: String(c.outgoing.port),
          tls: c.outgoing.tls,
          shared: c.outgoing.username === "",
          username: c.outgoing.username,
          password: "",
        }
      : empty.outgoing,
  };
}

/** What the API takes. A port that is not a number is sent as 0, for the server to name. */
function bodyOf(d: Draft): EmailConfigDraft {
  const o = d.outgoing;
  return {
    name: d.name,
    email: d.email,
    sender_name: d.sender_name,
    incoming: {
      ...d.incoming,
      port: Number(d.incoming.port) || 0,
    },
    outgoing: o.on
      ? {
          host: o.host,
          port: Number(o.port) || 0,
          tls: o.tls,
          username: o.shared ? "" : o.username,
          password: o.shared ? "" : o.password,
        }
      : null,
  };
}

/** A dialog adding an email config, or editing the one with id. */
export function ConfigForm({
  id,
  open,
  onClose,
}: {
  id: string | null;
  open: boolean;
  onClose: () => void;
}) {
  const configs = useQuery({
    queryKey: qk.emailConfigs,
    queryFn: getEmailConfigs,
    enabled: id !== null,
  });

  if (id === null) return <Form open={open} onClose={onClose} />;
  if (!configs.data) {
    return (
      <Dialog open={open} wide onClose={onClose} title="Edit mail account">
        <Dummy className="h-64 w-full" />
      </Dialog>
    );
  }
  const config = configs.data.find((c) => c.id === id);
  if (!config) {
    return (
      <Dialog open={open} wide onClose={onClose} title="Edit mail account">
        <p className="text-sm text-muted">There is no such mail account.</p>
      </Dialog>
    );
  }
  return <Form config={config} open={open} onClose={onClose} />;
}

function Form({
  config,
  open,
  onClose,
}: {
  config?: EmailConfig;
  open: boolean;
  onClose: () => void;
}) {
  const form = useId();
  const client = useQueryClient();
  const editing = config !== undefined;
  const [draft, setDraft] = useState<Draft>(() =>
    config ? fromConfig(config) : blank(),
  );
  // A new config's username is its address until somebody types one of their own.
  const [ownUsername, setOwnUsername] = useState(editing);

  const save = useMutation({
    mutationFn: () =>
      config
        ? putEmailConfigsById(config.id, bodyOf(draft))
        : postEmailConfigs(bodyOf(draft)),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: qk.emailConfigs });
      onClose();
    },
  });
  const test = useMutation({
    mutationFn: () =>
      config
        ? postEmailConfigsByIdTest(config.id, bodyOf(draft))
        : postEmailConfigsTest(bodyOf(draft)),
  });

  // A new config's servers, looked up from its address, fill the server fields while nobody has
  // typed in them: empty, or still as the last lookup left them.
  const latest = useRef(draft);
  latest.current = draft;
  const filled = useRef("");
  const [asked, setAsked] = useState("");
  const [note, setNote] = useState("");
  const lookup = useMutation({
    mutationFn: (email: string) => postEmailConfigsAutoconfig(email),
    onSuccess: (found, email) => {
      const domain = domainOf(email);
      if (!found.found) {
        setNote(
          found.oauth_only
            ? `${domain} signs in only with OAuth, and emguio signs in with a password alone.`
            : `No settings found for ${domain}. Your provider's help pages have them.`,
        );
        return;
      }
      const host = latest.current.incoming.host;
      if (host !== "" && host !== filled.current) {
        setNote("");
        return;
      }
      filled.current = found.incoming.host;
      setOwnUsername(found.incoming.username !== email);
      change((d) => withSettings(d, found));
      setNote(`${whence(found, domain)} Check them, then test the connection.`);
    },
    onError: (err) => setNote(messageOf(err)),
  });
  const lookUp = () => {
    const email = draft.email.trim();
    const domain = domainOf(email);
    if (editing || !domain.includes(".") || domain === asked) return;
    if (draft.incoming.host !== "" && draft.incoming.host !== filled.current) {
      return;
    }
    setAsked(domain);
    setNote("");
    lookup.mutate(email);
  };

  // Any change makes the last test's answer about something else.
  const change = (next: (d: Draft) => Draft) => {
    test.reset();
    setDraft(next);
  };
  const incoming = (patch: Partial<Draft["incoming"]>) =>
    change((d) => ({ ...d, incoming: { ...d.incoming, ...patch } }));
  const outgoing = (patch: Partial<Draft["outgoing"]>) =>
    change((d) => ({ ...d, outgoing: { ...d.outgoing, ...patch } }));

  // A port still at the usual one for the old setting moves to the usual one for the new; one
  // somebody typed stays.
  const secure = (side: "incoming" | "outgoing", tls: Security) => {
    const current = draft[side];
    const usual = ports[side][current.tls];
    const port =
      current.port === "" || Number(current.port) === usual
        ? String(ports[side][tls])
        : current.port;
    if (side === "incoming") incoming({ tls, port });
    else outgoing({ tls, port });
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };

  const kept = editing ? "Saved — leave empty to keep it" : undefined;

  return (
    <Dialog
      open={open}
      wide
      onClose={onClose}
      title={editing ? `Edit ${labelOf(config)}` : "Add mail account"}
      footer={
        <>
          {/* Beside the buttons that caused it, whatever the body is scrolled to. */}
          {test.data || test.error || save.error ? (
            <div className="flex w-full flex-col gap-1">
              {test.data ? <Results result={test.data} /> : null}
              {test.error ? (
                <p className="text-sm text-accent">{messageOf(test.error)}</p>
              ) : null}
              {save.error ? (
                <p className="text-sm text-accent">{messageOf(save.error)}</p>
              ) : null}
            </div>
          ) : null}
          <Button
            className="mr-auto"
            disabled={test.isPending}
            onClick={() => test.mutate()}
          >
            {test.isPending ? "Testing" : "Test connection"}
          </Button>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            type="submit"
            form={form}
            variant="solid"
            disabled={save.isPending}
          >
            Save
          </Button>
        </>
      }
    >
      <form id={form} className="flex flex-col gap-5" onSubmit={submit}>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Email address">
            <TextField
              type="email"
              autoComplete="off"
              placeholder="you@example.com"
              value={draft.email}
              onChange={(e) => {
                const email = e.target.value;
                change((d) => ({
                  ...d,
                  email,
                  incoming: ownUsername
                    ? d.incoming
                    : { ...d.incoming, username: email },
                }));
              }}
              onBlur={lookUp}
              data-autofocus={editing ? undefined : true}
            />
          </Field>
          <Field label="Name" hint="In the switcher. Empty shows the address.">
            <TextField
              placeholder="Work"
              value={draft.name}
              onChange={(e) => {
                const name = e.target.value;
                change((d) => ({ ...d, name }));
              }}
            />
          </Field>
        </div>

        {lookup.isPending || note ? (
          <p aria-live="polite" className="-mt-2 text-sm text-muted">
            {lookup.isPending ? `Looking up the settings for ${asked}…` : note}
          </p>
        ) : null}

        <Box legend="Incoming server">
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Protocol">
              <Select
                value={draft.incoming.protocol}
                onChange={(e) =>
                  incoming({ protocol: e.target.value as Protocol })
                }
              >
                <option value="imap">IMAP</option>
              </Select>
            </Field>
            <Field label="Security">
              <SecuritySelect
                value={draft.incoming.tls}
                onChange={(tls) => secure("incoming", tls)}
              />
            </Field>
          </div>
          <div className="grid gap-3 sm:grid-cols-[1fr_7rem]">
            <Field label="Host">
              <TextField
                placeholder="imap.example.com"
                autoCapitalize="off"
                spellCheck={false}
                value={draft.incoming.host}
                onChange={(e) => incoming({ host: e.target.value })}
              />
            </Field>
            <Field label="Port">
              <TextField
                inputMode="numeric"
                value={draft.incoming.port}
                onChange={(e) => incoming({ port: e.target.value })}
              />
            </Field>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Username">
              <TextField
                autoComplete="off"
                autoCapitalize="off"
                spellCheck={false}
                value={draft.incoming.username}
                onChange={(e) => {
                  setOwnUsername(true);
                  incoming({ username: e.target.value });
                }}
              />
            </Field>
            <Field label="Password">
              <TextField
                type="password"
                autoComplete="new-password"
                placeholder={kept}
                value={draft.incoming.password}
                onChange={(e) => incoming({ password: e.target.value })}
              />
            </Field>
          </div>
        </Box>

        <Box legend="Outgoing server">
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={draft.outgoing.on}
              onChange={(e) => outgoing({ on: e.target.checked })}
            />
            Send mail through an SMTP server
          </label>
          {draft.outgoing.on ? (
            <>
              <Field
                label="Your name"
                hint="Who mail sent from here is from, beside the address."
              >
                <TextField
                  autoComplete="name"
                  value={draft.sender_name}
                  onChange={(e) => {
                    const sender_name = e.target.value;
                    change((d) => ({ ...d, sender_name }));
                  }}
                />
              </Field>
              <div className="grid gap-3 sm:grid-cols-[1fr_7rem_10rem]">
                <Field label="Host">
                  <TextField
                    placeholder="smtp.example.com"
                    autoCapitalize="off"
                    spellCheck={false}
                    value={draft.outgoing.host}
                    onChange={(e) => outgoing({ host: e.target.value })}
                  />
                </Field>
                <Field label="Port">
                  <TextField
                    inputMode="numeric"
                    value={draft.outgoing.port}
                    onChange={(e) => outgoing({ port: e.target.value })}
                  />
                </Field>
                <Field label="Security">
                  <SecuritySelect
                    value={draft.outgoing.tls}
                    onChange={(tls) => secure("outgoing", tls)}
                  />
                </Field>
              </div>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={draft.outgoing.shared}
                  onChange={(e) => outgoing({ shared: e.target.checked })}
                />
                Sign in with the incoming username and password
              </label>
              {draft.outgoing.shared ? null : (
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field label="Username">
                    <TextField
                      autoComplete="off"
                      autoCapitalize="off"
                      spellCheck={false}
                      value={draft.outgoing.username}
                      onChange={(e) => outgoing({ username: e.target.value })}
                    />
                  </Field>
                  <Field label="Password">
                    <TextField
                      type="password"
                      autoComplete="new-password"
                      placeholder={
                        config?.outgoing?.username ? kept : undefined
                      }
                      value={draft.outgoing.password}
                      onChange={(e) => outgoing({ password: e.target.value })}
                    />
                  </Field>
                </div>
              )}
            </>
          ) : (
            <p className="text-sm text-muted">Optional.</p>
          )}
        </Box>
      </form>
    </Dialog>
  );
}

function SecuritySelect({
  value,
  onChange,
}: {
  value: Security;
  onChange: (tls: Security) => void;
}) {
  return (
    <Select
      value={value}
      onChange={(e) => onChange(e.target.value as Security)}
    >
      <option value="implicit">TLS</option>
      <option value="starttls">STARTTLS</option>
    </Select>
  );
}

function Box({ legend, children }: { legend: string; children: ReactNode }) {
  return (
    <fieldset className="flex flex-col gap-3 rounded-lg border border-line bg-bg p-4">
      <legend className="px-1 text-sm font-medium">{legend}</legend>
      {children}
    </fieldset>
  );
}

const domainOf = (email: string) =>
  email.slice(email.lastIndexOf("@") + 1).toLowerCase();

/** A draft with the servers a lookup found. */
function withSettings(d: Draft, s: Extract<Settings, { found: true }>): Draft {
  const o = s.outgoing;
  const shared = o !== null && o.username === s.incoming.username;
  return {
    ...d,
    incoming: {
      ...d.incoming,
      host: s.incoming.host,
      port: String(s.incoming.port),
      tls: s.incoming.tls,
      username: s.incoming.username,
    },
    outgoing: o
      ? {
          ...d.outgoing,
          on: true,
          host: o.host,
          port: String(o.port),
          tls: o.tls,
          shared,
          username: shared ? "" : o.username,
        }
      : d.outgoing,
  };
}

/** Where a lookup found what it filled in. */
function whence(s: Extract<Settings, { found: true }>, domain: string): string {
  switch (s.source) {
    case "provider":
      return `Filled in from ${domain}'s own settings.`;
    case "dns":
      return `Filled in from ${domain}'s DNS.`;
    default:
      return s.domain === domain
        ? "Filled in from Mozilla's list of providers."
        : `Filled in from Mozilla's list, for ${s.domain}, which handles ${domain}'s mail.`;
  }
}
