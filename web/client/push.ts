let worker: Promise<ServiceWorkerRegistration> | undefined;

export function pushWorker() {
  worker ??= navigator.serviceWorker.register("/sw.js", { updateViaCache: "none" }).then(() => navigator.serviceWorker.ready).catch(error => {
    worker = undefined;
    throw error;
  });
  return worker;
}

export function pushSupported() {
  return window.isSecureContext && "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
}

if (window.isSecureContext && "serviceWorker" in navigator) void pushWorker().catch(() => {});

export async function currentSubscription() {
  if (!pushSupported()) return null;
  const registration = await navigator.serviceWorker.getRegistration();
  return registration?.pushManager ? registration.pushManager.getSubscription() : null;
}

export async function pushAction<T>(action: string, params: Record<string, unknown> = {}): Promise<T> {
  const response = await fetch("/auth/push", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ action, ...params }), signal: AbortSignal.timeout(16_000),
  });
  const result = await response.json();
  if (!response.ok) throw new Error(result.error || "알림 요청을 처리하지 못했습니다.");
  return result as T;
}

export function applicationKey(value: string) {
  const key = atob(value.replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(key, character => character.charCodeAt(0));
}
