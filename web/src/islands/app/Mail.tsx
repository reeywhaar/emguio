import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";

import { getEmailConfigs } from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { buttonLook } from "@app/components/Button";
import { Dummy } from "@app/components/Dummy";
import { labelOf, pick, rememberConfig } from "@app/islands/app/emailConfig";
import { Link } from "@app/islands/app/Link";
import { paths } from "@app/islands/app/route";

/** One email config's mail. named is the config in the address, or null for the usual one. */
export function Mail({ named }: { named: string | null }) {
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
      <main className="flex flex-1 flex-col items-center justify-center gap-3 p-4">
        <p className="text-sm text-muted">No email configs yet.</p>
        <Link href={paths.newConfig} className={buttonLook("solid")}>
          Add an email config
        </Link>
      </main>
    );
  }

  if (!config) {
    return (
      <main className="flex flex-1 flex-col items-center justify-center gap-3 p-4">
        <p className="text-sm text-muted">There is no such email config.</p>
        <Link href={paths.mail()} className="text-sm underline">
          Open the usual one
        </Link>
      </main>
    );
  }

  return (
    <main className="flex flex-col gap-1 p-4">
      <h1 className="text-lg font-semibold">{labelOf(config)}</h1>
      <p className="text-sm text-muted">
        Nothing is read from {config.email} yet.
      </p>
    </main>
  );
}
