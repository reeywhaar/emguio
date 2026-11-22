import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { postAuthLogout } from "@app/api/actions/auth";
import {
  deleteSessions,
  deleteSessionsById,
  getSessions,
} from "@app/api/actions/sessions";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import { Button } from "@app/components/Button";
import { useConfirm } from "@app/components/Confirm";
import { Dummy } from "@app/components/Dummy";
import { when } from "@app/format";
import { say } from "@app/islands/app/notices";
import { leaveFor } from "@app/leave";
import { forget } from "@app/push";

/** Where the user is signed in, each to sign out, and all but this browser at once. */
export function Sessions() {
  const confirm = useConfirm();
  const client = useQueryClient();
  const sessions = useQuery({ queryKey: qk.sessions, queryFn: getSessions });
  const settled = () => client.invalidateQueries({ queryKey: qk.sessions });
  const revoke = useMutation({
    mutationFn: deleteSessionsById,
    onError: (err) => say(messageOf(err)),
    onSettled: settled,
  });
  // As signing out above: a device signed out of shows nobody's mail.
  const leave = useMutation({
    mutationFn: async () => {
      await forget();
      await postAuthLogout();
    },
    onSuccess: () => leaveFor("/login"),
    onError: (err) => say(messageOf(err)),
  });
  const revokeOthers = useMutation({
    mutationFn: deleteSessions,
    onError: (err) => say(messageOf(err)),
    onSettled: settled,
  });
  const others = (sessions.data ?? []).filter((s) => !s.current).length;

  return (
    <section className="flex flex-col gap-3">
      <h2 className="caps text-muted">Sessions</h2>
      <div className="flex flex-col gap-3 rounded-lg border border-line bg-bg p-4">
        {!sessions.data ? (
          <Dummy className="h-8 w-full" />
        ) : (
          <ul aria-label="Sessions" className="flex flex-col gap-2 text-sm">
            {sessions.data.map((s) => (
              <li
                key={s.id}
                className="flex items-center justify-between gap-3"
              >
                <span className="min-w-0">
                  {s.device || "A browser"}
                  {s.current ? (
                    <span className="text-muted"> · this browser</span>
                  ) : (
                    <span className="text-muted">
                      {" "}
                      · last used {when(s.last_seen_at)}
                    </span>
                  )}
                  {/* A guess, so what it was guessed from is under it. */}
                  <span
                    className="block truncate text-xs text-faint"
                    title={s.user_agent}
                  >
                    {s.user_agent}
                  </span>
                </span>
                <Button
                  size="bar"
                  className="shrink-0"
                  disabled={revoke.isPending || leave.isPending}
                  onClick={async () => {
                    if (!s.current) {
                      revoke.mutate(s.id);
                      return;
                    }
                    const yes = await confirm({
                      title: "Sign this browser out?",
                      message:
                        "This is the browser you are using. Signing it out takes you to the sign-in page.",
                      confirm: "Sign out",
                    });
                    if (yes) leave.mutate();
                  }}
                >
                  {s.current ? "Sign out…" : "Sign out"}
                </Button>
              </li>
            ))}
          </ul>
        )}
        {others > 0 ? (
          <div>
            <Button
              size="bar"
              disabled={revokeOthers.isPending}
              onClick={() => revokeOthers.mutate()}
            >
              Sign out everywhere else
            </Button>
          </div>
        ) : null}
      </div>
    </section>
  );
}
