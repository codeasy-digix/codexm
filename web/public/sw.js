const chatPath = /^\/chat\/[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i;
const destination = value => typeof value === "string" && chatPath.test(value) ? value : "/";

self.addEventListener("install", event => event.waitUntil(self.skipWaiting()));
self.addEventListener("activate", event => event.waitUntil(self.clients.claim()));

self.addEventListener("push", event => {
  let payload = {};
  try { payload = event.data?.json() ?? {}; } catch { /* Show a generic alert. */ }
  const url = destination(payload?.url);
  const test = payload?.test === true;
  const tag = typeof payload?.tag === "string" && /^[a-f0-9]{32}$/.test(payload.tag) ? payload.tag : undefined;
  event.waitUntil(self.registration.showNotification(test ? "CodexM 테스트 알림" : "CodexM 작업 완료", {
    body: test ? "이 기기에 알림이 도착했습니다." : "눌러서 채팅을 확인하세요.",
    icon: "/icons/icon-192.png",
    tag,
    data: { url },
  }));
});

self.addEventListener("notificationclick", event => {
  event.notification.close();
  const url = new URL(destination(event.notification.data?.url), self.location.origin);
  event.waitUntil((async () => {
    const windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    const open = windows.find(client => client.url === url.href);
    if (open) return open.focus();
    const existing = windows.find(client => new URL(client.url).origin === url.origin);
    if (existing) {
      try {
        const navigated = await existing.navigate(url.href);
        if (navigated) return navigated.focus();
      } catch { /* A new window can still open the chat. */ }
    }
    return self.clients.openWindow(url.href);
  })());
});
