import { useEffect, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getEmailConfigs,
  getEmailConfigsByIdMailboxes,
  postEmailConfigsByIdSync,
} from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import type { EmailConfig, Mailbox } from "@app/api/types";
import { Button, buttonLook } from "@app/components/Button";
import { Dummy } from "@app/components/Dummy";
import { Select } from "@app/components/Field";
import { Refresh, SelectMark } from "@app/components/icons";
import { ago } from "@app/format";
import { pick, rememberConfig } from "@app/islands/app/emailConfig";
import { Link } from "@app/islands/app/Link";
import { depthOf, labelOfMailbox, usual } from "@app/islands/app/mailbox";
import {
  MessageDummies,
  MessageList,
  type Selecting,
} from "@app/islands/app/MessageList";
import { Selection } from "@app/islands/app/Selection";
import { countsWithPending, usePending } from "@app/islands/app/pending";
import { Reader } from "@app/islands/app/Reader";
import { Search } from "@app/islands/app/Search";
import { go, paths } from "@app/islands/app/route";

/**
 * One email config's mail: its folders down the side and a folder's messages beside them.
 *
 * named, mailbox and message are what the address says: null for the usual config and folder,
 * and for no message open. q is what the folder is searched for, empty for none.
 */
export function Mail({
  named,
  mailbox,
  message,
  q = "",
}: {
  named: string | null;
  mailbox: string | null;
  message: string | null;
  q?: string;
}) {
  const configs = useQuery({
    queryKey: qk.emailConfigs,
    queryFn: getEmailConfigs,
  });
  const config = configs.data ? pick(configs.data, named) : undefined;

  useEffect(() => {
    if (config) rememberConfig(config.id);
  }, [config]);

  if (!configs.data) {
    return (
      <main className="flex flex-col gap-2 p-4">
        <Dummy className="h-6 w-48" />
        <Dummy className="h-4 w-64" />
      </main>
    );
  }
  if (configs.data.length === 0) {
    return (
      <Centered>
        <p className="text-sm text-muted">No email configs yet.</p>
        <Link href={paths.newConfig} className={buttonLook("solid")}>
          Add an email config
        </Link>
      </Centered>
    );
  }
  if (!config) {
    return (
      <Centered>
        <p className="text-sm text-muted">There is no such email config.</p>
        <Link href={paths.mail()} className="text-sm underline">
          Open the usual one
        </Link>
      </Centered>
    );
  }
  return (
    <Folders
      config={config}
      mailbox={mailbox}
      message={message}
      q={q}
      key={config.id}
    />
  );
}

/**
 * Three panes where there is room for them: folders, a folder's list, and the open message. On a
 * narrower screen the message takes the list's place, and on a phone the folders are a menu.
 */
function Folders({
  config,
  mailbox,
  message,
  q,
}: {
  config: EmailConfig;
  mailbox: string | null;
  message: string | null;
  q: string;
}) {
  const listed = useQuery({
    queryKey: qk.mailboxes(config.id),
    queryFn: () => getEmailConfigsByIdMailboxes(config.id),
  });
  // The counts as the server said, moved by the actions pressed and not yet answered.
  const pending = usePending(config.id);
  const boxes = {
    data: listed.data && countsWithPending(listed.data, pending),
  };
  // While messages are being selected, the ones that are; null while they are not. A folder's
  // own, so another folder starts with none.
  const [selected, setSelected] = useState<Set<string> | null>(null);
  const current = boxes.data
    ? mailbox
      ? boxes.data.find((mb) => mb.id === mailbox)
      : usual(boxes.data)
    : undefined;

  return (
    <div className="flex min-h-0 flex-1">
      <aside className="hidden w-60 shrink-0 flex-col border-r border-line bg-surface md:flex">
        <nav
          aria-label="Folders"
          className="relative flex-1 overflow-y-auto p-2"
        >
          {boxes.data ? (
            <ul className="flex flex-col">
              {boxes.data.map((mb) => (
                <li key={mb.id}>
                  <FolderLink
                    config={config.id}
                    mb={mb}
                    current={mb.id === current?.id}
                  />
                </li>
              ))}
            </ul>
          ) : (
            <div className="flex flex-col gap-2 p-1">
              <Dummy className="h-6 w-full" />
              <Dummy className="h-6 w-3/4" />
              <Dummy className="h-6 w-2/3" />
            </div>
          )}
        </nav>
      </aside>

      <section
        className={`min-w-0 flex-col lg:flex lg:w-[26rem] lg:flex-none lg:border-r lg:border-line ${message ? "hidden" : "flex flex-1"}`}
      >
        <div className="flex items-center gap-2 border-b border-line bg-bg px-4 py-2">
          {selected && current && boxes.data ? (
            <Selection
              config={config.id}
              mailbox={current}
              q={q}
              boxes={boxes.data}
              selected={selected}
              change={setSelected}
              done={() => setSelected(null)}
            />
          ) : null}
          {!selected && boxes.data && boxes.data.length > 0 ? (
            <Select
              aria-label="Folder"
              size="bar"
              className="min-w-0 flex-1 md:hidden"
              value={current?.id ?? ""}
              onChange={(e) => go(paths.mail(config.id, e.target.value))}
            >
              {current ? null : <option value="">Choose a folder</option>}
              {boxes.data.map((mb) => (
                <option key={mb.id} value={mb.id} disabled={!mb.selectable}>
                  {" ".repeat(depthOf(mb))}
                  {labelOfMailbox(mb)}
                  {mb.unseen ? ` (${mb.unseen})` : ""}
                </option>
              ))}
            </Select>
          ) : null}
          {selected ? null : (
            <>
              <h1 className="hidden min-w-0 flex-1 truncate font-semibold md:block">
                {current ? labelOfMailbox(current) : ""}
              </h1>
              {current?.selectable ? (
                // Selecting is about the list: an open message is closed for it.
                <Button
                  size="bar"
                  aria-label="Select messages"
                  title="Select messages"
                  onClick={() => {
                    setSelected(new Set());
                    go(paths.mail(config.id, current.id, undefined, q));
                  }}
                >
                  <SelectMark />
                </Button>
              ) : null}
              <SyncState config={config} />
            </>
          )}
        </div>
        {current?.selectable ? (
          <Search
            config={config.id}
            mailbox={current}
            q={q}
            key={`${current.id} ${q}`}
          />
        ) : null}
        {config.sync_error ? (
          <p
            role="alert"
            className="border-b border-line bg-bg px-4 py-2 text-sm text-accent"
          >
            {config.sync_error}{" "}
            <Link
              href={paths.editConfig(config.id)}
              className="whitespace-nowrap underline"
            >
              Check its settings
            </Link>
          </p>
        ) : null}
        <Body
          config={config}
          boxes={boxes.data}
          current={current}
          named={mailbox}
          open={message}
          q={q}
          select={selected ? { selected, change: setSelected } : undefined}
        />
      </section>

      {message && current ? (
        <Reader
          config={config.id}
          mailbox={current}
          message={message}
          q={q}
          key={message}
        />
      ) : (
        <div className="hidden flex-1 items-center justify-center lg:flex">
          <p className="text-sm text-faint">Choose a message to read it.</p>
        </div>
      )}
    </div>
  );
}

function Body({
  config,
  boxes,
  current,
  named,
  open,
  q,
  select,
}: {
  config: EmailConfig;
  boxes: Mailbox[] | undefined;
  current: Mailbox | undefined;
  named: string | null;
  open: string | null;
  q: string;
  select?: Selecting;
}) {
  if (!boxes) return <MessageDummies count={4} />;
  if (boxes.length === 0) {
    // Nothing yet and nothing wrong: the first look at the server has not finished.
    if (config.sync_error) return null;
    return (
      <div className="flex flex-col gap-3 p-4">
        <p className="text-sm text-muted">
          Fetching folders from {config.incoming.host}…
        </p>
        <MessageDummies count={4} />
      </div>
    );
  }
  if (!current) {
    return (
      <Centered>
        <p className="text-sm text-muted">
          {named ? "There is no such folder." : "No folder here can be opened."}
        </p>
      </Centered>
    );
  }
  if (!current.selectable) {
    return (
      <Centered>
        <p className="text-sm text-muted">
          This folder only holds other folders.
        </p>
      </Centered>
    );
  }
  return (
    <MessageList
      config={config.id}
      mailbox={current}
      open={open}
      q={q}
      select={select}
      key={`${current.id} ${q}`}
    />
  );
}

function FolderLink({
  config,
  mb,
  current,
}: {
  config: string;
  mb: Mailbox;
  current: boolean;
}) {
  const indent = { paddingLeft: `${0.75 + depthOf(mb) * 0.875}rem` };
  if (!mb.selectable) {
    return (
      <span
        className="flex min-h-8 items-center px-3 text-sm text-faint"
        style={indent}
      >
        {labelOfMailbox(mb)}
      </span>
    );
  }
  return (
    <Link
      href={paths.mail(config, mb.id)}
      aria-current={current ? "page" : undefined}
      className={`flex min-h-8 items-center gap-2 rounded-md px-3 text-sm ${current ? "bg-shade font-medium" : "hover:bg-shade"}`}
      style={indent}
    >
      <span className="min-w-0 flex-1 truncate">{labelOfMailbox(mb)}</span>
      {mb.unseen ? (
        <span className="text-xs font-medium text-brand">
          <span className="sr-only">, unread: </span>
          {mb.unseen}
        </span>
      ) : null}
    </Link>
  );
}

/** When the mail was last brought up to date, and a way to ask for it now. */
function SyncState({ config }: { config: EmailConfig }) {
  const client = useQueryClient();
  const sync = useMutation({
    mutationFn: () => postEmailConfigsByIdSync(config.id),
    // A folder other than INBOX is listed from the server, and the event stream says nothing
    // about it: asking for new mail asks for it again.
    onSuccess: () =>
      client.invalidateQueries({ queryKey: qk.lists(config.id) }),
  });
  return (
    <div className="flex shrink-0 items-center gap-2">
      <span className="hidden text-xs text-muted sm:inline">
        {config.synced_at
          ? `Updated ${ago(config.synced_at)}`
          : config.sync_error
            ? ""
            : "Not fetched yet"}
      </span>
      <Button
        size="bar"
        aria-label="Fetch new mail"
        title="Fetch new mail"
        disabled={sync.isPending}
        onClick={() => sync.mutate()}
      >
        <Refresh />
      </Button>
    </div>
  );
}

function Centered({ children }: { children: ReactNode }) {
  return (
    <main className="flex flex-1 flex-col items-center justify-center gap-3 p-4">
      {children}
    </main>
  );
}
