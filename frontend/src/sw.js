const SW_VERSION = "aswg-sw-v1";

self.addEventListener("install", (event) => {
  event.waitUntil(self.skipWaiting());
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener("push", (event) => {
  let payload = {};
  try {
    payload = event.data ? event.data.json() : {};
  } catch {
    payload = { title: "ASWG", body: event.data ? event.data.text() : "" };
  }

  const notification = payload?.notification || payload;
  const title = String(notification?.title || "ASWG 通知").trim();
  const body = String(notification?.body || notification?.preview || "").trim();
  const tag = String(notification?.tag || notification?.id || "aswg-push").trim();
  const targetURL = String(notification?.url || notification?.target_url || "/").trim() || "/";

  event.waitUntil(
    self.registration.showNotification(title, {
      body,
      tag,
      renotify: false,
      data: {
        url: targetURL,
        source: "aswg",
        version: SW_VERSION,
      },
    }),
  );
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const targetURL = String(event.notification?.data?.url || "/").trim() || "/";

  event.waitUntil((async () => {
    const nextURL = new URL(targetURL, self.location.origin);
    const clientList = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    for (const client of clientList) {
      if (!client || typeof client.focus !== "function") {
        continue;
      }
      const clientURL = new URL(client.url);
      if (clientURL.origin !== nextURL.origin) {
        continue;
      }
      if (typeof client.navigate === "function" && client.url !== nextURL.href) {
        try {
          await client.navigate(nextURL.href);
        } catch {
          // Ignore navigation failures and fallback to focusing current page.
        }
      }
      await client.focus();
      return;
    }
    if (self.clients.openWindow) {
      await self.clients.openWindow(nextURL.href);
    }
  })());
});
