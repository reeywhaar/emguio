import { request } from "@app/api/transport";
import type { Reading } from "@app/api/types";

export const getReading = () => request<Reading>("/api/reading");

/** Answered with what was kept. */
export const putReading = (body: Reading) =>
  request<Reading>("/api/reading", { method: "PUT", body });
