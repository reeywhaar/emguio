/**
 * Getting emguio's notifications onto this browser, and off it again.
 *
 * What the browser can do decides, never its user agent: in a Safari tab on iOS PushManager is
 * absent, and it appears once emguio is opened from the Home Screen. See docs/push.md.
 */

import {
  deleteDevicesById,
  getDevices,
  getPushKey,
  postDevices,
} from "@app/api/actions/push";
import { go } from "@app/islands/app/route";

export type PushState =
  /** This browser cannot show them at all. */
  | "unsupported"
  /** It could, from the Home Screen. */
  | "needs-install"
  /** Refused; only the browser's own settings undo it. */
  | "denied"
  /** Allowed. */
  | "granted"
  /** Not asked yet. */
  | "off";

export function pushSupported(): boolean {
  return (
    typeof navigator !== "undefined" &&
    "serviceWorker" in navigator &&
    typeof window !== "undefined" &&
    "PushManager" in window &&
    "Notification" in window
  );
}

/** Whether this page runs as an installed app rather than in a tab. */
export function installed(): boolean {
  if (window.matchMedia("(display-mode: standalone)").matches) return true;
  return (window.navigator as { standalone?: boolean }).standalone === true;
}

/** Whether this is iOS, to say what to do there. iPadOS says it is a Mac, with a touch screen. */
export function isIOS(): boolean {
  const ua = navigator.userAgent;
  if (/iPhone|iPad|iPod/.test(ua)) return true;
  return /Macintosh/.test(ua) && navigator.maxTouchPoints > 1;
}

export function pushState(): PushState {
  if (!pushSupported()) {
    return isIOS() && !installed() ? "needs-install" : "unsupported";
  }
  if (Notification.permission === "denied") return "denied";
  if (Notification.permission === "granted") return "granted";
  return "off";
}

const clientKey = "emguio.client";

/**
 * This browser's name for itself, kept in localStorage, which is per browser profile as a
 * subscription is. A renewed subscription with the same name replaces the old one's row.
 */
function clientID(): string {
  try {
    const kept = localStorage.getItem(clientKey);
    if (kept) return kept;
    const made = crypto.randomUUID();
    localStorage.setItem(clientKey, made);
    return made;
  } catch {
    // Empty replaces nothing, where a name two browsers shared would delete one's row.
    return "";
  }
}

/** The name, without making one: a page looked at does not give a browser an identity. */
export function storedClientID(): string {
  try {
    return localStorage.getItem(clientKey) ?? "";
  } catch {
    return "";
  }
}

/** What a person would call this device in a list. */
function label(): string {
  const ua = navigator.userAgent;
  const browser = /Firefox\//.test(ua)
    ? "Firefox"
    : /Edg\//.test(ua)
      ? "Edge"
      : /Chrome\//.test(ua)
        ? "Chrome"
        : /Safari\//.test(ua)
          ? "Safari"
          : "A browser";
  const platform = /iPhone/.test(ua)
    ? "iPhone"
    : /iPad/.test(ua) || isIOS()
      ? "iPad"
      : /Android/.test(ua)
        ? "Android"
        : /Macintosh/.test(ua)
          ? "Mac"
          : /Windows/.test(ua)
            ? "Windows"
            : /Linux/.test(ua)
              ? "Linux"
              : "";
  return platform ? `${browser} on ${platform}` : browser;
}

// Over an ArrayBuffer of its own: applicationServerKey refuses a view that might be shared.
function fromBase64Url(value: string): Uint8Array<ArrayBuffer> {
  const padded =
    value.replace(/-/g, "+").replace(/_/g, "/") +
    "=".repeat((4 - (value.length % 4)) % 4);
  const raw = atob(padded);
  const bytes = new Uint8Array(new ArrayBuffer(raw.length));
  for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i);
  return bytes;
}

function toBase64Url(buffer: ArrayBuffer | null): string {
  if (!buffer) return "";
  let raw = "";
  for (const b of new Uint8Array(buffer)) raw += String.fromCharCode(b);
  return btoa(raw).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

/** At the root, so its scope is every page. */
function registerWorker(): Promise<ServiceWorkerRegistration> {
  return navigator.serviceWorker.register("/sw.js", { scope: "/" });
}

async function send(subscription: PushSubscription): Promise<void> {
  await postDevices({
    endpoint: subscription.endpoint,
    p256dh: toBase64Url(subscription.getKey("p256dh")),
    auth: toBase64Url(subscription.getKey("auth")),
    label: label(),
    client_id: clientID(),
  });
}

/** Asks for permission and subscribes. From a click: iOS asks only then. */
export async function enable(): Promise<PushState> {
  if (!pushSupported()) return pushState();
  const permission = await Notification.requestPermission();
  if (permission !== "granted") return pushState();
  const registration = await registerWorker();
  const { key } = await getPushKey();
  const subscription = await registration.pushManager.subscribe({
    // Every browser requires it: a push that shows nothing is not one it hands out.
    userVisibleOnly: true,
    applicationServerKey: fromBase64Url(key),
  });
  await send(subscription);
  return "granted";
}

/**
 * Hands over whatever subscription this browser holds, each time the app opens. A browser renews
 * one without asking, and not every one says so to the worker.
 */
export async function refresh(): Promise<void> {
  if (!pushSupported() || Notification.permission !== "granted") return;
  try {
    const registration = await registerWorker();
    const subscription = await registration.pushManager.getSubscription();
    if (subscription) await send(subscription);
  } catch {
    // The next time it opens.
  }
}

/** Stops this browser showing the user's notifications: its device and its subscription. */
export async function forget(): Promise<void> {
  if (!pushSupported()) return;
  try {
    const mine = storedClientID();
    if (mine) {
      for (const d of await getDevices()) {
        if (d.client_id === mine) await deleteDevicesById(d.id);
      }
    }
    const registration = await navigator.serviceWorker.getRegistration("/");
    await (await registration?.pushManager.getSubscription())?.unsubscribe();
  } catch {
    // Left as it was; a subscription let go of later is dropped when its push service says so.
  }
}

/**
 * Answers the worker: who this browser is, when it renews a subscription on its own; and where to
 * go, when a notification is tapped while emguio is open.
 */
export function answerWorker(): void {
  if (!pushSupported()) return;
  navigator.serviceWorker.addEventListener("message", (event) => {
    const data = event.data as { ask?: string; open?: string } | null;
    if (data?.ask === "client-id") event.ports[0]?.postMessage(clientID());
    if (typeof data?.open === "string" && data.open.startsWith("/")) {
      go(data.open);
    }
  });
}
