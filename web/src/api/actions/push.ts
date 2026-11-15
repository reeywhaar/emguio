import { request } from "@app/api/transport";
import type { Device, PushSettings, Subscription } from "@app/api/types";

/** What a browser subscribes with. */
export const getPushKey = () => request<{ key: string }>("/api/push/key");

export const getDevices = () =>
  request<{ devices: Device[] }>("/api/devices").then((it) => it.devices);

/** Hands a subscription over; the same endpoint, or the same browser, keeps its one row. */
export const postDevices = (body: Subscription) =>
  request<Device>("/api/devices", { method: "POST", body });

export const deleteDevicesById = (id: string) =>
  request<void>(`/api/devices/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });

export const getPushSettings = () =>
  request<PushSettings>("/api/push/settings");

/** Answered with what was kept. */
export const putPushSettings = (body: PushSettings) =>
  request<PushSettings>("/api/push/settings", { method: "PUT", body });

/** Sends a notification to every device; answered with how many took it. */
export const postDevicesTest = () =>
  request<{ delivered: number }>("/api/devices/test", { method: "POST" });
