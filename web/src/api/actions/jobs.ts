import { request } from "@app/api/transport";
import type { Job, JobDraft } from "@app/api/types";

/** The user's jobs, waiting and failed, oldest first. */
export const getJobs = () =>
  request<{ jobs: Job[] }>("/api/jobs").then((it) => it.jobs);

/** Asks for something to be done to a message; answered as soon as it is queued. */
export const postJobs = (body: JobDraft) =>
  request<Job>("/api/jobs", { method: "POST", body });

/** Lets a failed job go, once it has been shown. */
export const deleteJobsById = (id: string) =>
  request<void>(`/api/jobs/${encodeURIComponent(id)}`, { method: "DELETE" });
