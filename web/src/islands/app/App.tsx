import { Boundary } from "@app/components/Boundary";
import { ConfigForm } from "@app/islands/app/ConfigForm";
import { Header } from "@app/islands/app/Header";
import { Link } from "@app/islands/app/Link";
import { Mail } from "@app/islands/app/Mail";
import { paths, useRoute, type Route } from "@app/islands/app/route";
import { Settings } from "@app/islands/app/Settings";

export function App() {
  const route = useRoute();
  return (
    <div className="flex min-h-dvh flex-col">
      <Header route={route} />
      <Boundary what="This page" key={route.page}>
        <Page route={route} />
      </Boundary>
    </div>
  );
}

function Page({ route }: { route: Route }) {
  switch (route.page) {
    case "mail":
      return <Mail named={route.config} />;
    case "settings":
      return <Settings />;
    case "new-config":
      return <ConfigForm />;
    case "edit-config":
      return <ConfigForm id={route.id} />;
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
