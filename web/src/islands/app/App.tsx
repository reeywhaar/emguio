import { useEffect, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { getAuthMe } from "@app/api/actions/auth";
import { qk } from "@app/api/keys";
import { useLive } from "@app/api/live";
import { whenSealed } from "@app/api/transport";
import { Boundary } from "@app/components/Boundary";
import { Compose } from "@app/islands/app/Compose";
import { Header } from "@app/islands/app/Header";
import { Link } from "@app/islands/app/Link";
import { Jobs } from "@app/islands/app/Jobs";
import { Mail } from "@app/islands/app/Mail";
import { Notice } from "@app/islands/app/Notice";
import { paths, useRoute, type Route } from "@app/islands/app/route";
import { Seal } from "@app/islands/app/Seal";
import { Settings } from "@app/islands/app/Settings";
import { Transfers } from "@app/islands/app/Transfers";

export function App() {
  const route = useRoute();
  const client = useQueryClient();
  const me = useQuery({ queryKey: qk.me, queryFn: getAuthMe });
  useLive();
  // A request refused as sealed — the server restarted under an open page — asks again who is
  // signed in, and the page below follows.
  useEffect(
    () => whenSealed(() => client.invalidateQueries({ queryKey: qk.me })),
    [client],
  );
  // Nothing of the user's is asked for until their seal key is open.
  if (!me.data) return null;
  if (me.data.seal !== "open") {
    return (
      <div className="fixed inset-0 flex flex-col overflow-y-auto">
        <Seal state={me.data.seal} />
        <Notice />
      </div>
    );
  }
  // Pinned to the window rather than as tall as it: the mail view scrolls its own list, and every
  // other page scrolls inside the space under the header, so the window itself has nothing to
  // scroll. A height of 100dvh can round a pixel or two past the screen in Mobile Safari, and
  // the window then scrolls by that much.
  return (
    <div className="fixed inset-0 flex flex-col overflow-hidden">
      <Header route={route} />
      <Boundary what="This page" key={route.page}>
        <Page route={route} />
      </Boundary>
      <Compose />
      <Jobs />
      <Transfers />
      <Notice />
    </div>
  );
}

function Page({ route }: { route: Route }) {
  switch (route.page) {
    case "mail":
      return (
        <Mail
          named={route.config}
          mailbox={route.mailbox}
          message={route.message}
          q={route.q}
        />
      );
    case "settings":
      return (
        <Scroll>
          <Settings editing={route.editing} />
        </Scroll>
      );
    case "missing":
      return (
        <main className="flex flex-1 flex-col items-center justify-center gap-3 p-4">
          <p className="text-sm text-muted">
            There is nothing at this address.
          </p>
          <Link href={paths.mail()} className="text-sm underline">
            Go to your mail
          </Link>
        </main>
      );
  }
}

function Scroll({ children }: { children: ReactNode }) {
  return (
    <div className="relative min-h-0 flex-1 overflow-y-auto">{children}</div>
  );
}
