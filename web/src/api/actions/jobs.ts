import { request } from "@app/api/transport";
import type { Job, JobDraft } from "@app/api/types";

/** The user's jobs, waiting and failed, oldest first. */
export const getJobs = () =>
  request<{ jobs: Job[] }>("/api/jobs").then((it) => it.jobs);

/** Asks for things to be done to messages, in order; answered as soon as they are queued. */
export const postJobs = (jobs: JobDraft[]) =>
  request<{ jobs: Job[] }>("/api/jobs", {
    method: "POST",
    body: { jobs },
  }).then((it) => it.jobs);

/** Lets a failed job go, once it has been shown. */
export const deleteJobsById = (id: string) =>
  request<void>(`/api/jobs/${encodeURIComponent(id)}`, { method: "DELETE" });
