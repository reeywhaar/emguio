import type { ReactNode } from "react";

import { useLive } from "@app/api/live";
import { Boundary } from "@app/components/Boundary";
import { Compose } from "@app/islands/app/Compose";
import { Header } from "@app/islands/app/Header";
import { Link } from "@app/islands/app/Link";
import { Jobs } from "@app/islands/app/Jobs";
import { Mail } from "@app/islands/app/Mail";
import { Notice } from "@app/islands/app/Notice";
import { paths, useRoute, type Route } from "@app/islands/app/route";
import { Settings } from "@app/islands/app/Settings";

export function App() {
  const route = useRoute();
  useLive();
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
