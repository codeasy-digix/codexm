import React, { useEffect, useState } from "react";
import { applicationKey, pushAction, pushSupported, pushWorker } from "../../client/push";

type Status = { registration: ServiceWorkerRegistration; publicKey: string; subscription: PushSubscription | null; subscribed: boolean };

export function NotificationSettings({ thread }: { thread?: string }) {
  const [status, setStatus] = useState<Status>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const supported = pushSupported();
  const standalone = matchMedia("(display-mode: standalone)").matches || (navigator as Navigator & { standalone?: boolean }).standalone;

  useEffect(() => {
    if (!supported) return;
    let closed = false;
    void (async () => {
      try {
        const registration = await pushWorker();
        const subscription = await registration.pushManager.getSubscription();
        const server = await pushAction<{ publicKey: string; subscribed: boolean }>("status", { endpoint: subscription?.endpoint });
        if (!closed) setStatus({ registration, subscription, ...server });
      } catch (reason) {
        if (!closed) setError((reason as Error).message);
      }
    })();
    return () => { closed = true; };
  }, [supported]);

  async function change(action: "enable" | "disable" | "test") {
    if (!status || busy) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      if (action === "enable") {
        // Start the browser permission flow directly from the user gesture.
        const subscription = status.subscription ?? await status.registration.pushManager.subscribe({
          userVisibleOnly: true, applicationServerKey: applicationKey(status.publicKey),
        });
        setStatus({ ...status, subscription });
        await pushAction("subscribe", { subscription: subscription.toJSON() });
        setStatus({ ...status, subscription, subscribed: true });
        setNotice("이 기기에서 완료 알림을 받습니다.");
      } else if (action === "disable") {
        await pushAction("unsubscribe", { endpoint: status.subscription?.endpoint });
        await status.subscription?.unsubscribe();
        setStatus({ ...status, subscription: null, subscribed: false });
        setNotice("이 기기의 완료 알림을 껐습니다.");
      } else {
        await pushAction("test", { endpoint: status.subscription?.endpoint, thread });
        setNotice("테스트 알림을 보냈습니다.");
      }
    } catch (reason) {
      setError(Notification.permission === "denied" ? "이 기기의 브라우저 설정에서 CodexM 알림을 허용해 주세요." : (reason as Error).message);
    } finally { setBusy(false); }
  }

  return <div className="dialog-form">
    <p>채팅 작업이 끝나면 이 기기로 알림을 보냅니다. 알림을 누르면 해당 채팅이 열립니다.</p>
    {!standalone && <p className="muted">Chrome 메뉴에서 앱을 설치하거나 홈 화면에 추가할 수 있습니다. iPhone이나 iPad에서는 홈 화면의 CodexM을 열어 알림을 켜세요.</p>}
    {!supported && standalone && <p className="muted">이 브라우저에서 웹 푸시 알림을 사용할 수 없습니다.</p>}
    {supported && !status && !error && <p role="status">알림 설정을 읽고 있습니다…</p>}
    {error && <p className="inline-error" role="alert">{error}</p>}
    {notice && <p role="status">{notice}</p>}
    {status && <div className="button-row">
      {status.subscribed ? <>
        <button type="button" disabled={busy} onClick={() => void change("disable")}>알림 끄기</button>
        <button type="button" className="primary" disabled={busy} onClick={() => void change("test")}>테스트 알림</button>
      </> : <button type="button" className="primary" disabled={busy} onClick={() => void change("enable")}>이 기기에서 알림 켜기</button>}
    </div>}
  </div>;
}
