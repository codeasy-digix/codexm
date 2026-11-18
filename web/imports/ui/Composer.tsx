import React, { useEffect, useRef, useState } from "react";
import { call, mutationUnconfirmed } from "../../client/data";
import { ChatQueue, type SubmissionOutcome } from "./ChatQueue";
import {
  composerAction, resolveTurnSelection,
  type ChatHistory, type ChatModel, type ChatThread, type LiveTurn, type TurnSelection,
} from "../contracts/web";

type Props = {
  chat: ChatThread;
  account: string;
  history?: ChatHistory;
  liveTurn?: Pick<LiveTurn, "id" | "status">;
  models: ChatModel[];
  selection: TurnSelection;
  onSent: () => void;
};

export function Composer({ chat, account, history, liveTurn, models, selection, onSent }: Props) {
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [uncertain, setUncertain] = useState(false);
  const inFlight = useRef(false);
  const relayKey = `codexm.guiRelay.${chat.id}`;
  const [relayID, setRelayID] = useState(() => sessionStorage.getItem(relayKey) || "");
  const relayText = useRef("");
  const refresh = useRef(onSent);
  refresh.current = onSent;
  const external = history?.thread.externalOwner;
  const delivery = history?.delivery;
  const guiRelay = external && delivery?.state === "relay";
  const relayingDraft = !!relayID && text === relayText.current;
  const active = external ? delivery?.state === "ready" || delivery?.state === "waiting"
    : liveTurn ? liveTurn.status === "inProgress" : history?.thread.status.type === "active";
  const turn = external ? delivery?.turnId : active
    ? liveTurn?.id ?? history?.turns.slice().reverse().find(t => t.status === "inProgress")?.id : undefined;
  const draining = !!history?.loginDraining;
  const queue = history?.queue || [];
  const { selectedModel, inheritedEffort } = resolveTurnSelection(chat, models, selection);
  const hasOverride = !!(selection.model || selection.effort);

  useEffect(() => {
    if (!relayID) return;
    let closed = false;
    let timer: ReturnType<typeof setTimeout>;
    async function check() {
      let status = "unknown";
      try {
        const result = await call<{ status: string }>("web.guiRelayStatus", { thread_id: chat.id, request_id: relayID });
        status = result.status;
      } catch { /* Retain the original input if the delivery result cannot be verified. */ }
      if (closed) return;
      if (status === "pending") {
        timer = setTimeout(() => void check(), 1500);
        return;
      }
      sessionStorage.removeItem(relayKey);
      setRelayID("");
      if (status === "accepted") {
        setText(current => current === relayText.current ? "" : current);
        setNotice("GUI 채팅에 전달했습니다.");
        refresh.current();
      } else if (status === "expired") {
        setError("중계 시간이 만료됐습니다. 입력은 작성창에 남아 있습니다.");
      } else if (status === "rejected") {
        setError("GUI에서 대상 채팅을 확인하지 못했습니다. 입력은 작성창에 남아 있습니다.");
      } else {
        setError("GUI에 전달됐는지 확인하지 못했습니다.");
        setUncertain(true);
      }
    }
    void check();
    return () => { closed = true; clearTimeout(timer); };
  }, [chat.id, relayID, relayKey]);

  async function send(mode: "immediate" | "queue", recalled?: string): Promise<SubmissionOutcome> {
    const sent = recalled ?? text;
    if (!sent.trim() || inFlight.current || uncertain) return "rejected";
    if (relayID && (mode === "immediate" || sent === relayText.current)) return "rejected";
    const parameters: Record<string, string> = { thread_id: chat.id, account, text: sent, mode };
    if (mode === "immediate" && hasOverride) {
      const model = selection.model || selectedModel?.model;
      const effort = selection.effort || inheritedEffort;
      if (model) parameters.model = model;
      if (effort) parameters.effort = effort;
    }
    inFlight.current = true;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const result = await call<{ mode: "start" | "steer" | "queue" | "relay"; relay_id?: string }>("web.send", parameters, crypto.randomUUID());
      if (result.mode === "relay" && result.relay_id) {
        relayText.current = sent;
        sessionStorage.setItem(relayKey, result.relay_id);
        setRelayID(result.relay_id);
        return "accepted";
      }
      if (recalled === undefined) setText(current => current === sent ? "" : current);
      if (mode === "queue") {
        setNotice(hasOverride ? "큐에 추가했습니다. 이 입력은 채팅의 실행 설정으로 처리됩니다." : "큐에 추가했습니다.");
      } else if (result.mode === "queue") {
        setNotice("새 작업 요청을 전달했습니다.");
      } else {
        setNotice(result.mode === "steer" ? "현재 작업에 전달했습니다." : "작업을 시작했습니다.");
      }
      onSent();
      return "accepted";
    } catch (e) {
      const message = (e as Error).message;
      setError(message);
      if (mutationUnconfirmed(message)) {
        setUncertain(true);
        return "unknown";
      }
      return "rejected";
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }

  async function stop() {
    if (!turn || inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError("");
    try {
      await call("web.interrupt", { thread_id: chat.id, account, turn_id: turn }, crypto.randomUUID());
      onSent();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }

  return <div className="composer-wrap">
    <ChatQueue thread={chat.id} account={account} items={queue} busy={busy} sendDisabled={uncertain}
      onChanged={onSent} onRequeue={input => send("queue", input)} />
    {external && delivery?.state !== "unavailable" && <p className="owner-note">{guiRelay ? "GUI 채팅에 해당 창의 계정으로 전달합니다." : "이 채팅을 열어 둔 창의 계정으로 전달합니다."}</p>}
    {draining && <p className="owner-note" role="status">기존 작업과 대기 입력이 끝나면 변경된 웹 계정이 자동 적용됩니다.</p>}
    {error && <p role="alert" className="inline-error">{error}</p>}
    {uncertain && <p className="inline-error">전송 결과가 불확실하여 재전송을 막았습니다. 대화와 큐에서 원래 입력을 확인하세요.</p>}
    <div className="composer">
      <textarea aria-label="메시지" value={text} onChange={e => setText(e.target.value)}
        placeholder={active ? "현재 작업에 개입하거나 다음 요청을 큐에 추가하세요" : "Codex에게 요청하세요"}
        rows={3} enterKeyHint="send" onKeyDown={event => {
          const mode = composerAction(event.nativeEvent);
          if (mode) {
            event.preventDefault();
            if (!event.repeat) void send(mode);
          }
        }} />
      <div className="composer-bottom">
        <small>Enter 전송·개입 · ⌘Enter 큐 · Shift+Enter 줄바꿈</small>
        <div className="composer-actions">
          {active && !external && <button className="stop-button" onClick={() => void stop()} disabled={busy} aria-label="현재 턴 중지">■</button>}
          <button aria-keyshortcuts="Meta+Enter" onClick={() => void send("queue")} disabled={busy || uncertain || !text.trim() || relayingDraft}>큐에 추가</button>
          <button className="primary" onClick={() => void send("immediate")} disabled={busy || !!relayID || uncertain || !text.trim()}>
            {busy || relayID ? "전달 중…" : "보내기"}
          </button>
        </div>
      </div>
    </div>
    {(relayID || notice) && <p role="status" className="delivery-notice">{relayID ? "GUI 채팅에 전달하고 있습니다…" : notice}</p>}
  </div>;
}
