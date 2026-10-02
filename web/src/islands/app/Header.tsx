import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";

import { getAuthMe, postAuthLogout } from "@app/api/actions/auth";
import { getEmailConfigs } from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import type { EmailConfig } from "@app/api/types";
import { Button, buttonLook } from "@app/components/Button";
import { Dialog } from "@app/components/Dialog";
import { Dummy } from "@app/components/Dummy";
import { Select } from "@app/components/Field";
import { PickerButton } from "@app/components/PickerButton";
import { labelOf, pick } from "@app/islands/app/emailConfig";
import { Link } from "@app/islands/app/Link";
import { go, paths, type Route } from "@app/islands/app/route";
import { leaveFor } from "@app/leave";

/**
 * The bar across the top: which email config is open, who is signed in, and the ways out.
 *
 * One config at a time, chosen here. Nothing anywhere shows two at once.
 */
export function Header({ route }: { route: Route }) {
  const me = useQuery({ queryKey: qk.me, queryFn: getAuthMe });
  const configs = useQuery({
    queryKey: qk.emailConfigs,
    queryFn: getEmailConfigs,
  });
  const signOut = useMutation({
    mutationFn: postAuthLogout,
    onSuccess: () => leaveFor("/login"),
  });

  const current = configs.data
    ? pick(configs.data, route.page === "mail" ? route.config : null)
    : undefined;

  return (
    <header className="flex items-center gap-2 border-b border-line bg-bg px-4 py-2 sm:gap-3">
      <Link href={paths.mail()} className="font-semibold">
        emguio
      </Link>
      {configs.data && configs.data.length > 0 ? (
        <>
          <ConfigPicker configs={configs.data} current={current} />
          <Select
            aria-label="Mail account"
            size="bar"
            className="hidden min-w-0 max-w-64 md:block"
            value={current?.id ?? ""}
            onChange={(e) => go(paths.mail(e.target.value))}
          >
            {current ? null : <option value="">Choose one</option>}
            {configs.data.map((c) => (
              <option key={c.id} value={c.id}>
                {labelOf(c)}
              </option>
            ))}
          </Select>
        </>
      ) : null}
      <span className="flex-1" />
      {/* Who is signed in is worth the room only where there is room. */}
      <span className="hidden sm:inline">
        {me.data ? (
          <span className="text-sm text-muted">{me.data.username}</span>
        ) : (
          <Dummy className="h-4 w-20" />
        )}
      </span>
      <Link
        href={paths.settings}
        className={`${buttonLook("quiet", "bar")} shrink-0 whitespace-nowrap`}
      >
        Settings
      </Link>
      {/* On a phone the switcher needs the room more, and Settings has a way out of its own.
          Hidden on a wrapper, because a button's own display would win over hidden. */}
      <span className="hidden shrink-0 sm:inline-flex">
        <Button
          size="bar"
          className="whitespace-nowrap"
          disabled={signOut.isPending}
          onClick={() => signOut.mutate()}
        >
          Sign out
        </Button>
      </span>
    </header>
  );
}

/** On a phone, the email config open, which opens the list of them to choose another. */
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
        className="max-w-64 md:hidden"
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
              </Link>
            </li>
          ))}
        </ul>
      </Dialog>
    </>
  );
}
