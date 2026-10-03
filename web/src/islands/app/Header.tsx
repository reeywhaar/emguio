import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { getAuthMe } from "@app/api/actions/auth";
import { getEmailConfigs } from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import type { EmailConfig } from "@app/api/types";
import { buttonLook } from "@app/components/Button";
import { Dialog } from "@app/components/Dialog";
import { Dummy } from "@app/components/Dummy";
import { ProfileMark } from "@app/components/icons";
import { PickerButton } from "@app/components/PickerButton";
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
        <ConfigPicker configs={configs.data} current={current} />
      ) : null}
      <span className="flex-1" />
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

/** The mail account open, which opens the list of them to choose another. */
function ConfigPicker({
  configs,
  current,
}: {
  configs: EmailConfig[];
  current: EmailConfig | undefined;
}) {
  const [open, setOpen] = useState(false);
  const close = () => setOpen(false);
  return (
    <>
      <PickerButton
        name="Mail account"
        plain
        className="max-w-64 font-medium"
        onClick={() => setOpen(true)}
      >
        <span className="min-w-0 flex-1 truncate">
          {current ? labelOf(current) : "Choose one"}
        </span>
      </PickerButton>
      <Dialog
        open={open}
        onClose={close}
        title="Mail accounts"
        footer={
          <Link
            href={paths.newConfig}
            className={buttonLook("quiet")}
            onClick={close}
          >
            Add mail account
          </Link>
        }
      >
        <ul className="flex flex-col">
          {configs.map((c) => (
            <li key={c.id}>
              <Link
                href={paths.mail(c.id)}
                aria-current={c.id === current?.id ? "page" : undefined}
                className={`flex min-h-8 items-center gap-2 rounded-md px-3 text-sm ${c.id === current?.id ? "bg-shade font-medium" : "hover:bg-shade"}`}
                onClick={close}
              >
                <span className="min-w-0 flex-1 truncate">{labelOf(c)}</span>
                {c.name ? (
                  <span className="truncate text-xs text-faint">{c.email}</span>
                ) : null}
                {c.inbox_unseen ? (
                  <span className="shrink-0 text-xs font-medium text-brand tabular-nums">
                    <span className="sr-only">, unread in Inbox: </span>
                    {c.inbox_unseen.toLocaleString()}
                  </span>
                ) : null}
              </Link>
            </li>
          ))}
        </ul>
      </Dialog>
    </>
  );
}
