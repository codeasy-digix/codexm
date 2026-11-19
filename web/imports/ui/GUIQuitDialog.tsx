import React, { useEffect, useRef, useState } from "react";
import { call } from "../../client/data";
import { Modal } from "./Modal";

type Preview = { confirmation: string; application: string };

export function GUIQuitDialog({ onClose, onStopped }: { onClose: () => void; onStopped: () => void }) {
  const [preview, setPreview] = useState<Preview>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const sending = useRef(false);
  const cancel = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    cancel.current?.focus();
    let open = true;
    void call<Preview>("web.guiQuitPreview").then(result => {
      if (open) setPreview(result);
    }).catch(reason => {
      if (open) setError((reason as Error).message);
    });
    return () => { open = false; };
  }, []);

  async function confirm() {
    if (!preview || sending.current) return;
    sending.current = true;
    setBusy(true);
    setPreview(undefined);
    try {
      await call("web.guiQuit", { confirmation: preview.confirmation }, crypto.randomUUID());
      onStopped();
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      sending.current = false;
      setBusy(false);
    }
  }

  return <Modal title="Codex GUI 전체 종료" onClose={() => { if (!sending.current) onClose(); }}>
    <div className="dialog-form">
      <p>이 Mac의 Codex GUI에서 진행 중인 모든 작업이 중단됩니다.</p>
      {preview && <p>종료할 앱 {preview.application}</p>}
      {error ? <p role="alert" className="inline-error">{error}</p> :
        busy ? <p role="status">GUI 종료를 확인하고 있습니다…</p> :
        !preview && <p role="status">실행 중인 Codex GUI를 확인하고 있습니다…</p>}
      <div className="button-row">
        <button type="button" ref={cancel} onClick={onClose} disabled={busy}>취소</button>
        <button type="button" className="danger-button" disabled={!preview || busy} onClick={() => void confirm()}>GUI 전체 종료</button>
      </div>
    </div>
  </Modal>;
}
