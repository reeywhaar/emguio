// emguio's service worker: shows what a push says, and opens what a tap is about.
//
// Plain JavaScript at the root, outside the bundle: its scope has to cover every page, and its
// address has to stay the same from one build to the next. See docs/push.md.

self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) =>
  event.waitUntil(self.clients.claim()),
);

self.addEventListener("push", (event) => {
  let data = {};
  try {
    data = event.data ? event.data.json() : {};
  } catch {
    // Shown all the same: a browser can take back a subscription whose pushes show nothing.
  }
  event.waitUntil(show(data));
});

/**
 * Shows one notification, replacing any with its tag. WebKit keeps the tag but does not replace
 * by it (https://bugs.webkit.org/show_bug.cgi?id=258922), so those are closed first.
 */
async function show(data) {
  const tag = data.tag || "emguio";
  try {
    for (const old of await self.registration.getNotifications({ tag })) {
      old.close();
    }
  } catch {
    // Not supported; the tag still asks.
  }
  await self.registration.showNotification(data.title || "emguio", {
    body: data.body || "",
    tag,
    renotify: true,
    icon: "/icon.png",
    data: { url: data.url || "/" },
  });
}

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const url = (event.notification.data && event.notification.data.url) || "/";
  event.waitUntil(
    (async () => {
      const windows = await self.clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      });
      for (const client of windows) {
        if (!("focus" in client)) continue;
        try {
          // Focused within the click, which is what lets it come to the front; then the page
          // goes there itself, without loading again.
          await client.focus();
          client.postMessage({ open: url });
          return;
        } catch {
          // Not this one; the next, or a window of its own.
        }
      }
      await self.clients.openWindow(url);
    })(),
  );
});

// A browser renews a subscription on its own. Not every browser says so, so the page also hands
// its subscription over each time it opens.
self.addEventListener("pushsubscriptionchange", (event) => {
  event.waitUntil(
    (async () => {
      try {
        const { key } = await (
          await fetch("/api/push/key", { credentials: "same-origin" })
        ).json();
        const subscription = await self.registration.pushManager.subscribe({
          userVisibleOnly: true,
          applicationServerKey: fromBase64Url(key),
        });
        const keys = subscription.toJSON().keys || {};
        await fetch("/api/devices", {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            endpoint: subscription.endpoint,
            p256dh: keys.p256dh || "",
            auth: keys.auth || "",
            label: "",
            // Or the new subscription is a second device beside the old.
            client_id: await clientID(),
          }),
        });
      } catch {
        // The page hands it over when it next opens.
      }
    })(),
  );
});

/**
 * The browser's id, which a page keeps in localStorage and a worker cannot read, so it asks a
 * window. With none open the answer is empty, which replaces nothing.
 */
async function clientID() {
  const windows = await self.clients.matchAll({
    type: "window",
    includeUncontrolled: true,
  });
  for (const client of windows) {
    const answer = await new Promise((resolve) => {
      const channel = new MessageChannel();
      channel.port1.onmessage = (e) => resolve(e.data);
      setTimeout(() => resolve(""), 500);
      client.postMessage({ ask: "client-id" }, [channel.port2]);
    });
    if (answer) return answer;
  }
  return "";
}

function fromBase64Url(value) {
  const padded =
    value.replace(/-/g, "+").replace(/_/g, "/") +
    "=".repeat((4 - (value.length % 4)) % 4);
  return Uint8Array.from(atob(padded), (c) => c.charCodeAt(0));
}
