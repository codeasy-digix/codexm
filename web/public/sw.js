const chatPath = /^\/chat\/[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}\/?$/i;

function destination(value) {
  if (typeof value === "string") {
    try {
      const url = new URL(value, self.location.origin);
      if (url.origin === self.location.origin && chatPath.test(url.pathname)) {
        return url.pathname.replace(/\/$/, "").toLowerCase();
      }
    } catch { /* Malformed routes fall back to the catalog. */ }
  }
  return "/";
}

function selectChat(client, path) {
  return new Promise(resolve => {
    const { port1, port2 } = new MessageChannel();
    const finish = accepted => {
      clearTimeout(timer);
      port1.close();
      resolve(accepted);
    };
    const timer = setTimeout(() => finish(false), 500);
    port1.onmessage = event => finish(event.data === true);
    try { client.postMessage({ type: "codexm:notification", path }, [port2]); }
    catch {
      port2.close();
      finish(false);
    }
  });
}

self.addEventListener("install", event => event.waitUntil(self.skipWaiting()));
self.addEventListener("activate", event => event.waitUntil(self.clients.claim()));

self.addEventListener("push", event => {
  let payload = {};
  try { payload = event.data?.json() ?? {}; } catch { /* Show a generic alert. */ }
  const notification = payload?.notification ?? payload;
  const url = destination(notification?.navigate ?? payload?.url);
  const title = typeof notification?.title === "string" && notification.title.trim() ? notification.title :
    payload?.test === true ? "테스트 알림" : "작업 완료";
  const tag = typeof notification?.tag === "string" && /^[a-f0-9]{32}$/.test(notification.tag) ? notification.tag : undefined;
  event.waitUntil(self.registration.showNotification(title, {
    body: typeof notification?.body === "string" ? notification.body : undefined,
    icon: "/icons/icon-192.png",
    tag,
    data: { url },
  }));
});

self.addEventListener("notificationclick", event => {
  event.notification.close();
  const url = new URL(destination(event.notification.data?.url), self.location.origin);
  event.waitUntil((async () => {
    const windows = (await self.clients.matchAll({ type: "window", includeUncontrolled: true }))
      .filter(client => client.frameType !== "nested" && new URL(client.url).origin === url.origin);
    const existing = windows.find(client => client.url === url.href) ?? windows[0];
    if (existing) {
      // A WindowClient can still report the URL from before SPA navigation.
      // Let the focused app select the chat and acknowledge its actual route.
      try {
        const focused = await existing.focus();
        if (await selectChat(focused, url.pathname)) return;
      } catch { /* Fall back to navigation if the old window is unavailable. */ }
      try {
        const navigated = await existing.navigate(url.href);
        if (navigated && destination(navigated.url) === url.pathname) return navigated.focus();
      } catch { /* A new window can still open the chat. */ }
    }
    const opened = await self.clients.openWindow(url.href);
    if (opened && destination(opened.url) !== url.pathname) await selectChat(opened, url.pathname);
  })());
});
