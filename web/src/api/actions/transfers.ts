import { request } from "@app/api/transport";
import type { Transfer } from "@app/api/types";

/** The user's moves to another account, moving and finished, oldest first. */
export const getTransfers = () =>
  request<{ transfers: Transfer[] }>("/api/transfers").then(
    (it) => it.transfers,
  );

/** Leaves the messages still waiting where they are; what has moved stays moved. */
export const postTransfersByIdCancel = (id: string) =>
  request<Transfer>(`/api/transfers/${encodeURIComponent(id)}/cancel`, {
    method: "POST",
  });

/** Sends again the messages a stopped or cancelled transfer left where they were. */
export const postTransfersByIdRetry = (id: string) =>
  request<Transfer>(`/api/transfers/${encodeURIComponent(id)}/retry`, {
    method: "POST",
  });

/** Lets a finished transfer go, once it has been shown. */
export const deleteTransfersById = (id: string) =>
  request<void>(`/api/transfers/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
