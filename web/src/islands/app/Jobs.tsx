import { useEffect } from "react";
import {
  useMutationState,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";

import { deleteJobsById, getJobs } from "@app/api/actions/jobs";
import { mk, qk } from "@app/api/keys";
import type { Job } from "@app/api/types";
import { say } from "@app/islands/app/notices";
import {
  accepted,
  commit,
  failure,
  labels,
  type Pending,
} from "@app/islands/app/pending";

/** How often the jobs are read while some wait, should the event stream be down. */
const POLL = 5000;

/**
 * The user's jobs, kept in step with the server, and what is still being done, in the corner.
 *
 * Each read compares the jobs with the last: one gone is done, and is written into what the
 * server said before the list that drew it pending changes; one failed is said and let go.
 */
export function Jobs() {
  const client = useQueryClient();
  const jobs = useQuery({
    queryKey: qk.jobs,
    queryFn: async () => {
      const before = client.getQueryData<Job[]>(qk.jobs) ?? [];
      const now = await getJobs();
      const still = new Set(now.map((j) => j.id));
      for (const j of before) {
        if (!j.error && !still.has(j.id)) commit(client, j);
      }
      // Newly failed: said once, by what it is called or by how many, and let go.
      const failed = now.filter(
        (j) => j.error && !before.some((b) => b.id === j.id && b.error),
      );
      if (failed.length > 0) {
        say(failure(failed, labels.get(failed[0]!.id), failed[0]!.error));
        for (const j of failed) deleteJobsById(j.id).catch(() => {});
      }
      return now;
    },
    refetchInterval: (q) =>
      (q.state.data ?? []).some((j) => !j.error) ? POLL : false,
  });

  // On their way: asked, and not yet taken by the server.
  const sending = useMutationState({
    filters: { mutationKey: mk.actions, status: "pending" },
    select: (m) => (m.state.variables as Pending[]).map((p) => p.ref),
  })
    .flat()
    .filter((ref) => !(ref && accepted.has(ref))).length;
  const waiting = (jobs.data ?? []).filter((j) => !j.error).length + sending;

  // Only what has not reached the server is lost with the tab; what has is done regardless.
  useEffect(() => {
    if (sending === 0) return;
    const stay = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", stay);
    return () => window.removeEventListener("beforeunload", stay);
  }, [sending]);

  if (waiting === 0) return null;
  return (
    <div
      role="status"
      className="fixed bottom-4 left-4 z-10 flex items-center gap-2 rounded-full border border-line bg-bg px-3 py-1.5 text-xs text-muted shadow-sm"
    >
      <span
        aria-hidden="true"
        className="size-3 animate-spin rounded-full border-2 border-line border-t-brand"
      />
      {waiting === 1 ? "Saving a change" : `Saving ${waiting} changes`}
    </div>
  );
}
