import {
  Fragment,
  useEffect,
  useReducer,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getEmailConfigs,
  getEmailConfigsByIdMailboxes,
  postEmailConfigsByIdSync,
  putEmailConfigsByIdMailboxesOrder,
} from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { EmailConfig, Mailbox } from "@app/api/types";
import { Button, buttonLook } from "@app/components/Button";
import { Dummy } from "@app/components/Dummy";
import { Dialog } from "@app/components/Dialog";
import {
  AllMailMark,
  ArchiveMark,
  DraftMark,
  FilterMark,
  InboxMark,
  PlainFolderMark,
  PlusMark,
  Refresh,
  SelectMark,
  SentMark,
  SpamMark,
  StarMark,
  TrashMark,
  WriteMark,
} from "@app/components/icons";
import { PickerButton } from "@app/components/PickerButton";
import { ago } from "@app/format";
import { useCarry } from "@app/islands/app/carry";
import { blank, write } from "@app/islands/app/drafts";
import { labelOf, pick, rememberConfig } from "@app/islands/app/emailConfig";
import { Link } from "@app/islands/app/Link";
import { FolderDialog } from "@app/islands/app/FolderDialog";
import { FolderMenu } from "@app/islands/app/FolderMenu";
import {
  arranged,
  besideOf,
  depthOf,
  labelOfMailbox,
  serversOwn,
  usual,
  within,
} from "@app/islands/app/mailbox";
import {
  MessageDummies,
  MessageList,
  type Selecting,
} from "@app/islands/app/MessageList";
import { say } from "@app/islands/app/notices";
import { Selection } from "@app/islands/app/Selection";
import { countsWithPending, usePending } from "@app/islands/app/pending";
import { Reader } from "@app/islands/app/Reader";
import { Search, queryOf, textOf, unreadOnly } from "@app/islands/app/Search";
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
        <p className="text-sm text-muted">No mail accounts yet.</p>
        <Link href={paths.newConfig} className={buttonLook("solid")}>
          Add a mail account
        </Link>
      </Centered>
    );
  }
  if (!config) {
    return (
      <Centered>
        <p className="text-sm text-muted">There is no such mail account.</p>
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
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "c" || e.metaKey || e.ctrlKey || e.altKey) return;
      const el = e.target instanceof HTMLElement ? e.target : null;
      if (el?.closest("input, textarea, select, [contenteditable='true']")) {
        return;
      }
      if (document.querySelector("dialog[open]")) return;
      e.preventDefault();
      write(blank(config.id));
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [config.id]);

  // While messages are being selected, the ones that are; null while they are not. A folder's
  // own, so another folder starts with none.
  const [selected, setSelected] = useState<Set<string> | null>(null);
  // Leaving select mode takes away what had focus: it goes back to the button that started it.
  const selectButton = useRef<HTMLButtonElement>(null);
  const selecting = selected !== null;
  const wasSelecting = useRef(selecting);
  useEffect(() => {
    if (wasSelecting.current && !selecting) selectButton.current?.focus();
    wasSelecting.current = selecting;
  }, [selecting]);
  const current = boxes.data
    ? mailbox
      ? boxes.data.find((mb) => mb.id === mailbox)
      : usual(boxes.data)
    : undefined;

  // The tab says where it is: the folder, then the account. Back to the page's own on leaving.
  const title = [current && labelOfMailbox(current), labelOf(config)]
    .filter(Boolean)
    .join(" | ");
  useEffect(() => {
    const before = document.title;
    document.title = title;
    return () => {
      document.title = before;
    };
  }, [title]);

  return (
    <div className="flex min-h-0 flex-1">
      <aside className="hidden w-60 shrink-0 flex-col border-r border-line bg-surface md:flex">
        <nav
          aria-label="Folders"
          className="relative flex-1 overflow-y-auto p-2"
        >
          {boxes.data ? (
            <FolderList
              config={config.id}
              boxes={boxes.data}
              current={current}
            />
          ) : (
            <div className="flex flex-col gap-2 p-1">
              <Dummy className="h-6 w-full" />
              <Dummy className="h-6 w-3/4" />
              <Dummy className="h-6 w-2/3" />
            </div>
          )}
        </nav>
        <div className="flex flex-col gap-1 border-t border-line px-3 py-2">
          <SyncState config={config} />
          <Credits />
        </div>
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
            <FolderPicker
              config={config}
              boxes={boxes.data}
              current={current}
            />
          ) : null}
          {selected ? null : (
            <>
              <h1 className="hidden min-w-0 flex-1 truncate font-semibold md:block">
                {current ? labelOfMailbox(current) : ""}
              </h1>
              {current && current.special_use === "" && boxes.data ? (
                <FolderMenu
                  config={config.id}
                  boxes={boxes.data}
                  folder={current}
                />
              ) : null}
              <Button
                size="bar"
                aria-label="Write a message"
                title="Write a message (c)"
                onClick={() => write(blank(config.id))}
              >
                <WriteMark />
              </Button>
              {current?.selectable ? (
                <Button
                  size="bar"
                  aria-label="Unread only"
                  aria-pressed={unreadOnly(q)}
                  title={
                    unreadOnly(q) ? "Show all mail" : "Show unread mail only"
                  }
                  onClick={() =>
                    go(
                      paths.mail(
                        config.id,
                        current.id,
                        undefined,
                        queryOf(textOf(q), !unreadOnly(q)),
                      ),
                    )
                  }
                >
                  <FilterMark on={unreadOnly(q)} />
                </Button>
              ) : null}
              {current?.selectable ? (
                // Selecting is about the list: an open message is closed for it.
                <Button
                  ref={selectButton}
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
          <p className="text-sm text-faint">Choose a message</p>
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

/**
 * The folders as a tree, and a way to make one; onPick is told when one is chosen or made, for a
 * list that closes then. The server's own come first and stay; one of the user's is dragged up
 * or down among the user's beside it, and what is inside it goes along.
 */
function FolderList({
  config,
  boxes,
  current,
  onPick,
}: {
  config: string;
  boxes: Mailbox[];
  current: Mailbox | undefined;
  onPick?: () => void;
}) {
  const client = useQueryClient();
  // The folder being carried, and the one beside it that it would go before or after.
  const [carrying, setCarrying] = useState<string | null>(null);
  const [onto, setOnto] = useState<string | null>(null);
  const arrange = useMutation({
    mutationFn: (ids: string[]) =>
      putEmailConfigsByIdMailboxesOrder(config, ids),
    onMutate: async (ids) => {
      await client.cancelQueries({ queryKey: qk.mailboxes(config) });
      const before = client.getQueryData<Mailbox[]>(qk.mailboxes(config));
      if (before) {
        client.setQueryData(qk.mailboxes(config), arranged(before, ids));
      }
      return before;
    },
    onError: (err, _ids, before) => {
      if (before) client.setQueryData(qk.mailboxes(config), before);
      say(messageOf(err));
    },
    onSuccess: (now) => client.setQueryData(qk.mailboxes(config), now),
  });

  const own = new Set(
    boxes.filter((mb) => serversOwn(boxes, mb)).map((mb) => mb.id),
  );
  const ours = (mb: Mailbox) =>
    besideOf(boxes, mb).filter((other) => !own.has(other.id));
  const carried = boxes.find((mb) => mb.id === carrying);
  const target = boxes.find((mb) => mb.id === onto);
  const beside = carried ? ours(carried).map((mb) => mb.id) : [];
  const down =
    carried && target
      ? beside.indexOf(carried.id) < beside.indexOf(target.id)
      : false;
  // Going down it lands after the target and all inside it, so the bar goes under the last of
  // those; going up, over the target. At the target's depth either way.
  const marked = target
    ? down
      ? boxes.filter((mb) => mb === target || within(mb, target)).at(-1)
      : target
    : undefined;

  const over = (mb: Mailbox, id: string | null) => {
    setCarrying(mb.id);
    const under = boxes.find((other) => other.id === id);
    setOnto(
      (under &&
        ours(mb).find(
          (other) =>
            other.id !== mb.id &&
            (other.id === under.id || within(under, other)),
        )?.id) ??
        null,
    );
  };
  // A line between the server's own and the user's beside them.
  const firstOfOurs = (mb: Mailbox) => {
    if (own.has(mb.id)) return false;
    const row = besideOf(boxes, mb);
    const before = row[row.indexOf(mb) - 1];
    return before !== undefined && own.has(before.id);
  };
  const drop = () => {
    setCarrying(null);
    setOnto(null);
    if (!carried || !target) return;
    const next = beside.filter((id) => id !== carried.id);
    next.splice(beside.indexOf(target.id), 0, carried.id);
    arrange.mutate(next);
  };

  return (
    <ul className="flex flex-col">
      {boxes.map((mb) => (
        <Fragment key={mb.id}>
          {firstOfOurs(mb) ? (
            <li
              aria-hidden="true"
              className="my-1 mr-3 border-t border-line"
              style={{ marginLeft: `${0.75 + depthOf(mb) * 0.875}rem` }}
            />
          ) : null}
          <li
            className={`relative ${carried && (mb === carried || within(mb, carried)) ? "opacity-40" : ""}`}
          >
            <FolderLink
              config={config}
              mb={mb}
              fixed={own.has(mb.id)}
              current={mb.id === current?.id}
              onPick={onPick}
              onOver={(id) => over(mb, id)}
              onDrop={drop}
            />
            {/* Absolute, so it takes no room and the rows do not shift under the pointer. */}
            {target && mb === marked ? (
              <span
                aria-hidden="true"
                className={`pointer-events-none absolute right-3 h-0.5 rounded-full bg-fg ${down ? "-bottom-px" : "-top-px"}`}
                style={{ left: `${0.75 + depthOf(target) * 0.875}rem` }}
              />
            ) : null}
          </li>
        </Fragment>
      ))}
      <li>
        <MakeFolder config={config} boxes={boxes} onMade={onPick} />
      </li>
    </ul>
  );
}

/** On a phone, the folder open, which opens the folders to choose another. */
function FolderPicker({
  config,
  boxes,
  current,
}: {
  config: EmailConfig;
  boxes: Mailbox[];
  current: Mailbox | undefined;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <PickerButton
        name="Folder"
        className="flex-1 md:hidden"
        onClick={() => setOpen(true)}
      >
        <span className="min-w-0 flex-1 truncate">
          {current ? labelOfMailbox(current) : "Choose a folder"}
        </span>
        {current ? <Counts mb={current} /> : null}
      </PickerButton>
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title="Folders"
        footer={
          <div className="flex w-full flex-col gap-1">
            <SyncState config={config} />
            <Credits />
          </div>
        }
      >
        <FolderList
          config={config.id}
          boxes={boxes}
          current={current}
          onPick={() => setOpen(false)}
        />
      </Dialog>
    </>
  );
}

/** What is unread of a folder, and how many it holds. */
function Counts({ mb }: { mb: Mailbox }) {
  if (!mb.messages) return null;
  return (
    <span className="text-xs tabular-nums">
      {mb.unseen ? (
        <span className="font-medium text-brand">
          <span className="sr-only">, unread: </span>
          {mb.unseen.toLocaleString()}
        </span>
      ) : null}
      <span className="text-faint">
        <span className="sr-only">, messages: </span>
        {mb.unseen ? <span aria-hidden="true">/</span> : null}
        {mb.messages.toLocaleString()}
      </span>
    </span>
  );
}

function FolderLink({
  config,
  mb,
  fixed,
  current,
  onPick,
  onOver,
  onDrop,
}: {
  config: string;
  mb: Mailbox;
  /** The server's own, which stays where it is. */
  fixed: boolean;
  current: boolean;
  onPick?: () => void;
  /** Carried over the folder with this id, or over none. */
  onOver: (id: string | null) => void;
  onDrop: () => void;
}) {
  const carry = useCarry({
    find: "[data-folder]",
    enabled: !fixed,
    onOver: (el) => onOver(el?.dataset.folder ?? null),
    onDrop,
  });
  const carrying = {
    "data-folder": mb.id,
    ref: carry.hold,
    draggable: false,
    onPointerDown: carry.press,
    onPointerMove: carry.move,
    onPointerUp: carry.release,
    onPointerCancel: carry.cancel,
    onContextMenu: carry.menu,
  };
  // No selecting, and no long-press preview, of a row a finger may be picking up.
  const still = fixed ? "" : "select-none [-webkit-touch-callout:none]";
  const indent = { paddingLeft: `${0.75 + depthOf(mb) * 0.875}rem` };
  if (!mb.selectable) {
    return (
      <span
        {...carrying}
        className={`flex min-h-8 items-center gap-2 px-3 text-sm text-faint ${still}`}
        style={indent}
      >
        <FolderIcon mb={mb} />
        {labelOfMailbox(mb)}
      </span>
    );
  }
  return (
    <Link
      {...carrying}
      href={paths.mail(config, mb.id)}
      aria-current={current ? "page" : undefined}
      className={`flex min-h-8 items-center gap-2 rounded-md px-3 text-sm ${still} ${current ? "bg-shade font-medium" : "hover:bg-shade"}`}
      style={indent}
      onClick={(e) => {
        if (carry.spent.current) {
          carry.spent.current = false;
          e.preventDefault();
          return;
        }
        onPick?.();
      }}
    >
      <span className="shrink-0 text-muted">
        <FolderIcon mb={mb} />
      </span>
      <span className="min-w-0 flex-1 truncate">{labelOfMailbox(mb)}</span>
      <Counts mb={mb} />
    </Link>
  );
}

/** What a folder is for, drawn: the server's own each by its use, the rest alike. */
function FolderIcon({ mb }: { mb: Mailbox }) {
  switch (mb.special_use) {
    case "inbox":
      return <InboxMark />;
    case "drafts":
      return <DraftMark />;
    case "sent":
      return <SentMark />;
    case "archive":
      return <ArchiveMark />;
    case "all":
      return <AllMailMark />;
    case "flagged":
      return <StarMark filled={false} />;
    case "junk":
      return <SpamMark />;
    case "trash":
      return <TrashMark />;
    default:
      return <PlainFolderMark />;
  }
}

/** Who made it, and where it lives. */
function Credits() {
  const link = "underline-offset-2 hover:text-muted hover:underline";
  return (
    <p className="text-[10px] text-faint">
      by{" "}
      <a
        href="https://vyrtsev.com"
        target="_blank"
        rel="noreferrer"
        className={link}
      >
        Misha Vyrtsev
      </a>{" "}
      (
      <a
        href="https://github.com/reeywhaar/emguio"
        target="_blank"
        rel="noreferrer"
        className={link}
      >
        GitHub
      </a>
      )
    </p>
  );
}

/** How long a look asked for is waited on before the button stops saying so. */
const LOOK_WAIT = 60_000;

/** When the mail was last brought up to date, and a way to ask for it now. */
function SyncState({ config }: { config: EmailConfig }) {
  // The config as it was when a look was asked for: the look is done once it is not.
  const [asked, setAsked] = useState<EmailConfig | null>(null);
  const waiting =
    asked !== null &&
    config.synced_at === asked.synced_at &&
    config.sync_error === asked.sync_error;
  useEffect(() => {
    if (!asked) return;
    const id = setTimeout(() => setAsked(null), LOOK_WAIT);
    return () => clearTimeout(id);
  }, [asked]);
  // "Updated a minute ago" goes on getting older with nothing else changing.
  const [, tick] = useReducer((n: number) => n + 1, 0);
  useEffect(() => {
    const id = setInterval(tick, 30_000);
    return () => clearInterval(id);
  }, []);

  return (
    <div className="flex min-w-0 flex-1 items-center gap-2">
      <span className="min-w-0 flex-1 truncate text-xs text-muted">
        {waiting
          ? "Updating…"
          : config.synced_at
            ? `Updated ${ago(config.synced_at)}`
            : config.sync_error
              ? ""
              : "Not fetched yet"}
      </span>
      <FetchMail
        config={config}
        waiting={waiting}
        onAsk={() => setAsked(config)}
        onFail={() => setAsked(null)}
      />
    </div>
  );
}

/** A new folder on the mail server, opened once it is made: last in the list, quieter than it. */
function MakeFolder({
  config,
  boxes,
  onMade,
}: {
  config: string;
  boxes: Mailbox[];
  onMade?: () => void;
}) {
  const [making, setMaking] = useState(false);
  // Each opening starts the form afresh.
  const [opened, setOpened] = useState(0);
  return (
    <>
      <button
        type="button"
        aria-label="New folder"
        className="flex min-h-8 w-full items-center gap-2 rounded-md px-3 text-sm text-faint hover:bg-shade hover:text-muted"
        onClick={() => {
          setOpened((n) => n + 1);
          setMaking(true);
        }}
      >
        <PlusMark />
        New
      </button>
      <FolderDialog
        key={opened}
        config={config}
        boxes={boxes}
        open={making}
        onClose={() => setMaking(false)}
        onDone={(made) => {
          setMaking(false);
          onMade?.();
          go(paths.mail(config, made.id));
        }}
      />
    </>
  );
}

function FetchMail({
  config,
  waiting,
  onAsk,
  onFail,
}: {
  config: EmailConfig;
  /** A look asked for is underway. */
  waiting: boolean;
  onAsk: () => void;
  onFail: () => void;
}) {
  const client = useQueryClient();
  const sync = useMutation({
    mutationFn: () => postEmailConfigsByIdSync(config.id),
    onMutate: onAsk,
    onError: onFail,
    // A search is the server's, and the event stream says nothing about it: asking for new mail
    // asks for it again.
    onSuccess: () =>
      client.invalidateQueries({ queryKey: qk.lists(config.id) }),
  });
  return (
    <Button
      size="bar"
      aria-label="Fetch new mail"
      title="Fetch new mail"
      disabled={sync.isPending || waiting}
      onClick={() => sync.mutate()}
    >
      <Refresh className={waiting ? "motion-safe:animate-spin" : ""} />
    </Button>
  );
}

function Centered({ children }: { children: ReactNode }) {
  return (
    <main className="flex flex-1 flex-col items-center justify-center gap-3 p-4">
      {children}
    </main>
  );
}
