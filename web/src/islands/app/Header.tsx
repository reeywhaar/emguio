import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { getAuthMe } from "@app/api/actions/auth";
import { getEmailConfigs } from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import type { EmailConfig } from "@app/api/types";
import { Dummy } from "@app/components/Dummy";
import { OverflowMark, PlusMark, ProfileMark } from "@app/components/icons";
import { Menu } from "@app/components/Menu";
import { Wordmark } from "@app/components/Wordmark";
import { labelOf, pick, turnedOn } from "@app/islands/app/emailConfig";
import { markUnread } from "@app/islands/app/favicon";
import { Link } from "@app/islands/app/Link";
import { fit } from "@app/islands/app/overflow";
import { paths, type Route } from "@app/islands/app/route";

/**
 * The bar across the top: which email config is open, and who is signed in, which opens
 * Settings.
 *
 * One config at a time, chosen here. Nothing anywhere shows two at once.
 */
export function Header({ route }: { route: Route }) {
  const me = useQuery({ queryKey: qk.me, queryFn: getAuthMe });
  const configs = useQuery({
    queryKey: qk.emailConfigs,
    queryFn: getEmailConfigs,
  });
  const on = configs.data && turnedOn(configs.data);
  // Away from the mail, none is open: each goes to its mail.
  const current =
    on && route.page === "mail" ? pick(on, route.config) : undefined;
  const unread = on?.some((c) => c.unseen > 0) ?? false;
  useEffect(() => markUnread(unread), [unread]);

  return (
    <header className="flex items-center gap-2 border-b border-line bg-bg px-4 py-2 sm:gap-3">
      <Link href={paths.mail()} className="font-semibold">
        <Wordmark />
      </Link>
      {on && on.length > 0 ? (
        <Accounts configs={on} current={current} />
      ) : (
        <span className="flex min-w-0 flex-1 items-center">
          {configs.data?.length === 0 ? (
            <Link
              href={paths.newConfig}
              className={`${tab} text-muted hover:bg-fill hover:text-fg`}
            >
              <PlusMark />
              Add mail account
            </Link>
          ) : null}
        </span>
      )}
      <Link
        href={paths.settings}
        title="Settings"
        aria-current={route.page === "settings" ? "page" : undefined}
        className="-mr-2 inline-flex min-h-8 min-w-0 items-center gap-1.5 rounded-md px-2 text-sm text-muted hover:bg-fill hover:text-fg aria-[current=page]:text-fg"
      >
        <span className="shrink-0">
          <ProfileMark />
        </span>
        <span className="sr-only">Settings, signed in as</span>{" "}
        {/* Who is signed in is worth the room only where there is room. */}
        {me.data ? (
          <span className="sr-only sm:not-sr-only">{me.data.username}</span>
        ) : (
          <Dummy className="hidden h-4 w-20 sm:block" />
        )}
      </Link>
    </header>
  );
}

/** The room between two accounts in the row: its gap-1. */
const GAP = 4;

/** What the row has room for, and what each part of it takes. */
type Sizes = { room: number; widths: Record<string, number>; button: number };

// Closer together on a phone, where the row is short of room.
const tab =
  "inline-flex min-h-8 items-center gap-1.5 rounded-md px-1.5 text-sm whitespace-nowrap sm:px-2.5";

/** The open account in the row, drawn as the others are but marked. */
const open = `${tab} bg-shade font-medium hover:bg-fill`;

/**
 * An account as the header names it: by its name, or else by its address, the part before the @
 * over the rest, on two short lines rather than one long one.
 */
function Label({ config }: { config: EmailConfig }) {
  const at = config.email.lastIndexOf("@");
  if (config.name || at <= 0) return <>{labelOf(config)}</>;
  return (
    <span className="flex flex-col text-left text-xs leading-tight">
      <span className="font-semibold text-fg">{config.email.slice(0, at)}</span>
      <span className="text-muted">{config.email.slice(at)}</span>
    </span>
  );
}

/**
 * The mail accounts side by side, as many as fit, and the rest behind a button after them. The
 * open one is always among them, and opens the list of them all; where not even it fits beside the
 * button, it is all there is. What fits is measured, since that depends on what they are called; a
 * copy of the row is drawn unseen to measure it.
 */
function Accounts({
  configs,
  current,
}: {
  configs: EmailConfig[];
  current: EmailConfig | undefined;
}) {
  const room = useRef<HTMLDivElement>(null);
  const copy = useRef<HTMLDivElement>(null);
  const [sizes, setSizes] = useState<Sizes | null>(null);
  useEffect(() => {
    const area = room.current;
    const all = copy.current;
    if (!area || !all || typeof ResizeObserver === "undefined") return;
    const measure = () => {
      const widths: Record<string, number> = {};
      for (const el of all.querySelectorAll<HTMLElement>("[data-config]")) {
        widths[el.dataset.config!] = el.getBoundingClientRect().width;
      }
      const button = all.querySelector("[data-rest]");
      setSizes({
        room: area.getBoundingClientRect().width,
        widths,
        button: button?.getBoundingClientRect().width ?? 0,
      });
    };
    const watch = new ResizeObserver(measure);
    watch.observe(area);
    watch.observe(all);
    return () => watch.disconnect();
  }, [current?.id]);
  const shown =
    sizes && (configs.length > 1 || !current)
      ? fit({
          ids: configs.map((c) => c.id),
          ...sizes,
          gap: GAP,
          current: current?.id,
        })
      : [];
  const rest = configs.filter((c) => !shown.includes(c.id));
  return (
    <div ref={room} className="relative flex min-w-0 flex-1 items-center">
      {/* Each as it is drawn in the row, and the button for the rest with its dot. */}
      <div
        ref={copy}
        aria-hidden="true"
        className="invisible absolute flex w-max items-center"
      >
        {configs.map((c) =>
          c.id === current?.id ? (
            <span key={c.id} data-config={c.id} className={open}>
              <Label config={c} />
              <Unread config={c} />
            </span>
          ) : (
            <span key={c.id} data-config={c.id} className={tab}>
              <Label config={c} />
              <Unread config={c} />
            </span>
          ),
        )}
        <span data-rest className="flex h-8 items-center gap-1 px-2">
          <OverflowMark />
          <span className="size-2" />
        </span>
      </div>
      {shown.length > 0 ? (
        <nav aria-label="Mail accounts" className="flex items-center gap-1">
          {configs
            .filter((c) => shown.includes(c.id))
            .map((c) =>
              c.id === current?.id ? (
                <ConfigPicker key={c.id} configs={configs} current={c} inRow />
              ) : (
                <Link
                  key={c.id}
                  href={paths.mail(c.id)}
                  className={`${tab} text-muted hover:bg-fill hover:text-fg`}
                >
                  <Label config={c} />
                  <Unread config={c} />
                </Link>
              ),
            )}
          {rest.length > 0 ? <Rest configs={rest} /> : null}
        </nav>
      ) : (
        <ConfigPicker configs={configs} current={current} />
      )}
    </div>
  );
}

/** The mail accounts there was no room for beside the others, marked while one has unread mail. */
function Rest({ configs }: { configs: EmailConfig[] }) {
  const unread = configs.some((c) => c.unseen > 0);
  return (
    <Menu
      name="Other mail accounts"
      title="Other mail accounts"
      plain
      chevron={false}
      end
      className="text-muted hover:text-fg"
      label={
        <>
          <OverflowMark />
          {unread ? (
            <>
              <span className="sr-only">, unread in one</span>
              <span
                aria-hidden="true"
                className="size-2 shrink-0 rounded-full bg-brand"
              />
            </>
          ) : null}
        </>
      }
    >
      {(close) => <Items configs={configs} close={close} />}
    </Menu>
  );
}

/** How many are unread in the folders a config notifies from, when any are. */
function Unread({ config }: { config: EmailConfig }) {
  if (!config.unseen) return null;
  return (
    <span className="shrink-0 text-xs font-medium text-brand tabular-nums">
      <span className="sr-only">, unread: </span>
      {config.unseen.toLocaleString()}
    </span>
  );
}

/**
 * The mail account open, which opens the list of them beside it to choose another. On its own it
 * is marked while another has unread mail; in the row, the others say so themselves.
 */
function ConfigPicker({
  configs,
  current,
  inRow = false,
}: {
  configs: EmailConfig[];
  current: EmailConfig | undefined;
  inRow?: boolean;
}) {
  const elsewhere = configs.some((c) => c.id !== current?.id && c.unseen > 0);
  return (
    <Menu
      name="Mail account"
      plain
      chevron={!inRow}
      look={inRow ? open : undefined}
      className={inRow ? "" : "max-w-64 font-medium"}
      label={
        inRow && current ? (
          <>
            <Label config={current} />
            <Unread config={current} />
          </>
        ) : (
          <>
            <span className="min-w-0 flex-1 truncate">
              {current ? <Label config={current} /> : "Choose one"}
            </span>
            {elsewhere ? (
              <>
                <span className="sr-only">, unread in another</span>
                <span
                  aria-hidden="true"
                  className="size-2 shrink-0 rounded-full bg-brand"
                />
              </>
            ) : null}
          </>
        )
      }
    >
      {(close) => <Items configs={configs} current={current} close={close} />}
    </Menu>
  );
}

const item = "flex min-h-8 items-center gap-2 rounded-md px-3 text-sm";

/** Mail accounts as a menu lists them: by name, with the address beside a name, and unread. */
function Items({
  configs,
  current,
  close,
}: {
  configs: EmailConfig[];
  current?: EmailConfig;
  close: () => void;
}) {
  return configs.map((c) => (
    <Link
      key={c.id}
      role="menuitem"
      href={paths.mail(c.id)}
      aria-current={c.id === current?.id ? "page" : undefined}
      className={`${item} ${c.id === current?.id ? "bg-shade font-medium" : "hover:bg-shade"}`}
      onClick={close}
    >
      <span className="min-w-0 flex-1 truncate">{labelOf(c)}</span>
      {c.name ? (
        <span className="truncate text-xs text-faint">{c.email}</span>
      ) : null}
      <Unread config={c} />
    </Link>
  ));
}
