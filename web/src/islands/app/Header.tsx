import { useMutation, useQuery } from "@tanstack/react-query";

import { getAuthMe, postAuthLogout } from "@app/api/actions/auth";
import { getEmailConfigs } from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { Button, buttonLook } from "@app/components/Button";
import { Dummy } from "@app/components/Dummy";
import { Select } from "@app/components/Field";
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
        <Select
          aria-label="Email config"
          size="bar"
          className="min-w-0 max-w-64"
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
