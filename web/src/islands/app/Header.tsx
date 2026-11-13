import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { getAuthMe } from "@app/api/actions/auth";
import { getEmailConfigs } from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import type { EmailConfig } from "@app/api/types";
import { Dummy } from "@app/components/Dummy";
import { ProfileMark } from "@app/components/icons";
import { Menu } from "@app/components/Menu";
import { Wordmark } from "@app/components/Wordmark";
import { labelOf, pick } from "@app/islands/app/emailConfig";
import { Link } from "@app/islands/app/Link";
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
  const current = configs.data
    ? pick(configs.data, route.page === "mail" ? route.config : null)
    : undefined;

  return (
    <header className="flex items-center gap-2 border-b border-line bg-bg px-4 py-2 sm:gap-3">
      <Link href={paths.mail()} className="font-semibold">
        <Wordmark />
      </Link>
      {configs.data && configs.data.length > 0 ? (
        <Accounts configs={configs.data} current={current} />
      ) : (
        <span className="flex-1" />
      )}
      <Link
        href={paths.settings}
        title="Settings"
        aria-current={route.page === "settings" ? "page" : undefined}
        className="inline-flex min-h-8 min-w-0 items-center gap-1.5 rounded-md px-2 text-sm text-muted hover:bg-fill hover:text-fg aria-[current=page]:text-fg pointer-coarse:min-h-10"
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

/**
 * Every mail account side by side where they fit, and otherwise the one open, which opens the list
 * of them. Whether they fit is measured, since that depends on what they are called; the row is
 * drawn unseen to measure it.
 */
function Accounts({
  configs,
  current,
}: {
  configs: EmailConfig[];
  current: EmailConfig | undefined;
}) {
  const room = useRef<HTMLDivElement>(null);
  const row = useRef<HTMLElement>(null);
  const [fits, setFits] = useState(false);
  useEffect(() => {
    const area = room.current;
    const all = row.current;
    if (!area || !all || typeof ResizeObserver === "undefined") return;
    const measure = () => setFits(all.scrollWidth <= area.clientWidth);
    const watch = new ResizeObserver(measure);
    watch.observe(area);
    watch.observe(all);
    return () => watch.disconnect();
  }, []);
  const side = fits && configs.length > 1;
  return (
    <div ref={room} className="relative flex min-w-0 flex-1 items-center">
      <nav
        ref={row}
        aria-label="Mail accounts"
        aria-hidden={side ? undefined : true}
        className={`flex w-max shrink-0 items-center gap-1 ${side ? "" : "invisible absolute"}`}
      >
        {configs.map((c) => (
          <Link
            key={c.id}
            href={paths.mail(c.id)}
            aria-current={c.id === current?.id ? "page" : undefined}
            className={`inline-flex min-h-8 items-center gap-1.5 rounded-md px-2.5 text-sm whitespace-nowrap pointer-coarse:min-h-10 ${c.id === current?.id ? "bg-shade font-medium" : "text-muted hover:bg-fill hover:text-fg"}`}
          >
            {labelOf(c)}
            <Unread config={c} />
          </Link>
        ))}
      </nav>
      {side ? null : <ConfigPicker configs={configs} current={current} />}
    </div>
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
 * The mail account open, which opens the list of them beside it to choose another, and is marked
 * while another has unread mail.
 */
function ConfigPicker({
  configs,
  current,
}: {
  configs: EmailConfig[];
  current: EmailConfig | undefined;
}) {
  const elsewhere = configs.some((c) => c.id !== current?.id && c.unseen > 0);
  const item =
    "flex min-h-8 items-center gap-2 rounded-md px-3 text-sm pointer-coarse:min-h-10";
  return (
    <Menu
      name="Mail account"
      plain
      className="max-w-64 font-medium"
      label={
        <>
          <span className="min-w-0 flex-1 truncate">
            {current ? labelOf(current) : "Choose one"}
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
      }
    >
      {(close) => (
        <>
          {configs.map((c) => (
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
          ))}
          <div role="separator" className="my-1 border-t border-line" />
          <Link
            role="menuitem"
            href={paths.newConfig}
            className={`${item} text-muted hover:bg-shade hover:text-fg`}
            onClick={close}
          >
            Add mail account
          </Link>
        </>
      )}
    </Menu>
  );
}
