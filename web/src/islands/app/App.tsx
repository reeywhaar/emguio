import { useMutation, useQuery } from "@tanstack/react-query";

import { getAuthMe, postAuthLogout } from "@app/api/actions/auth";
import { qk } from "@app/api/keys";
import { Button } from "@app/components/Button";
import { Dummy } from "@app/components/Dummy";
import { leaveFor } from "@app/leave";

export function App() {
  return (
    <div className="flex min-h-dvh flex-col">
      <Header />
      <main className="flex flex-1 items-center justify-center p-4">
        <p className="text-sm text-muted">No email configs yet.</p>
      </main>
    </div>
  );
}

/** The bar across the top: what this is, who is signed in, and the way out. */
function Header() {
  const me = useQuery({ queryKey: qk.me, queryFn: getAuthMe });
  const signOut = useMutation({
    mutationFn: postAuthLogout,
    onSuccess: () => leaveFor("/login"),
  });

  return (
    <header className="flex items-center gap-3 px-4 py-2">
      <span className="font-semibold">emguio</span>
      <span className="flex-1" />
      {me.data ? (
        <span className="text-sm text-muted">{me.data.username}</span>
      ) : (
        <Dummy className="h-4 w-20" />
      )}
      <Button
        size="bar"
        disabled={signOut.isPending}
        onClick={() => signOut.mutate()}
      >
        Sign out
      </Button>
    </header>
  );
}
