import { useMutation, useQueryClient } from "@tanstack/react-query";

import { postJobs } from "@app/api/actions/jobs";
import { mk, qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Job } from "@app/api/types";
import { say } from "@app/islands/app/notices";
import {
  accepted,
  draftOf,
  failure,
  labels,
  nextRef,
  type Pending,
} from "@app/islands/app/pending";

/**
 * Asks for jobs on one config's messages: drawn on the press by the pending layer, sent to the
 * server, which takes them at once and does them in the order asked whether or not this page is
 * still open. Sent one request at a time, so the server takes them in the order pressed; see
 * docs/reading.md.
 */
export function useAsk(config: string) {
  const client = useQueryClient();
  const send = useMutation({
    mutationKey: mk.actionsOf(config),
    scope: { id: `action:${config}` },
    mutationFn: (asked: Pending[]) => postJobs(asked.map(draftOf)),
    onSuccess: (jobs, asked) => {
      jobs.forEach((job, i) => {
        const p = asked[i];
        if (p?.label) labels.set(job.id, p.label);
        if (p?.ref) accepted.set(p.ref, job.id);
      });
      client.setQueryData<Job[]>(qk.jobs, (old) => [...(old ?? []), ...jobs]);
    },
    onError: (err, asked) =>
      say(failure(asked, asked[0]?.label, messageOf(err))),
  });
  return (asked: Omit<Pending, "ref">[]) => {
    if (asked.length > 0)
      send.mutate(asked.map((p) => ({ ...p, ref: nextRef() })));
  };
}
