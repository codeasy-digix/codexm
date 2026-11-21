import React, { useEffect, useRef, useState } from "react";
import { call, mutationUnconfirmed } from "../../client/data";
import { ChatQueue, type SubmissionOutcome } from "./ChatQueue";
import { PromptEditor } from "./PromptEditor";
import { emptyPromptDraft, readPromptDraft, settleStoredPrompt, writePromptDraft, type PromptDraft } from "./prompt-draft";
import {
  composerAction, queueModifierForPlatform, resolveTurnSelection,
  type ChatCommand, type ChatHistory, type ChatModel, type ChatThread, type LiveTurn, type TurnSelection,
} from "../contracts/web";

type Props = {
  chat: ChatThread;
  online: boolean;
  account: string;
  pendingAccountChange?: boolean;
  switchAccount: boolean;
  suggestion?: string;
  history?: ChatHistory;
  liveTurn?: Pick<LiveTurn, "id" | "status">;
  models: ChatModel[];
  selection: TurnSelection;
  onCommand: (command: ChatCommand) => void;
  onSent: (id?: string) => void;
};

export function Composer({ chat, online, account, history, liveTurn, models, selection, onCommand, onSent, pendingAccountChange, switchAccount, suggestion }: Props) {
  const queueModifier = queueModifierForPlatform(navigator.platform);
  const queueShortcut = queueModifier === "Meta" ? "⌘Enter" : "Ctrl+Enter";
  const [draft, setDraft] = useState(() => readPromptDraft(chat.id));
  const draftRef = useRef(draft);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  function save(next: PromptDraft) {
    draftRef.current = next;
    writePromptDraft(chat.id, next);
    setDraft(next);
  }
  const [action, setAction] = useState<"immediate" | "queue" | "stop">();
  const busy = action !== undefined;
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const inFlight = useRef(false);
  const relayKey = `codexm.guiRelay.${chat.id}`;
  const [relayID, setRelayID] = useState(() => sessionStorage.getItem(relayKey) || "");
  const relayText = useRef(relayID ? draft.text : "");
  const relayOperation = useRef(relayID ? draft.pending : undefined);
  const refresh = useRef(onSent);
  refresh.current = onSent;
  const external = history?.thread.externalOwner;
  const delivery = history?.delivery;
  const guiRelay = external && delivery?.state === "relay";
  const relayingDraft = !!relayID && draft.text === relayText.current;
  const uncertain = !!draft.pending && !busy && !relayingDraft;
  const active = external ? delivery?.state === "ready" || delivery?.state === "waiting"
    : liveTurn ? liveTurn.status === "inProgress" : history?.thread.status.type === "active";
  const turn = external ? delivery?.turnId : active
    ? liveTurn?.id ?? history?.turns.slice().reverse().find(t => t.status === "inProgress")?.id : undefined;
  const draining = !!history?.loginDraining;
  const queue = history?.queue || [];
  const { selectedModel, inheritedEffort } = resolveTurnSelection(chat, models, selection);
  const hasOverride = !!(selection.model || selection.effort);

  function settle(operation: string, accepted: boolean) {
    settleStoredPrompt(chat.id, operation, accepted);
    if (draftRef.current.pending !== operation) return;
    const next = accepted ? emptyPromptDraft : { ...draftRef.current, pending: undefined };
    draftRef.current = next;
    if (mounted.current) setDraft(next);
  }

  useEffect(() => {
    if (!relayID || !online) return;
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
      const operation = relayOperation.current;
      if (status === "accepted") {
        if (operation) settle(operation, true);
        setNotice("GUI 채팅에 전달했습니다.");
        refresh.current();
      } else if (status === "expired") {
        if (operation) settle(operation, false);
        setError("중계 시간이 만료됐습니다. 입력은 작성창에 남아 있습니다.");
      } else if (status === "rejected") {
        if (operation) settle(operation, false);
        setError("GUI에서 대상 채팅을 확인하지 못했습니다. 입력은 작성창에 남아 있습니다.");
      } else {
        setError("GUI에 전달됐는지 확인하지 못했습니다.");
      }
    }
    void check();
    return () => { closed = true; clearTimeout(timer); };
  }, [chat.id, relayID, relayKey, online]);

  async function send(mode: "immediate" | "queue", recalled?: PromptDraft): Promise<SubmissionOutcome> {
    const sent = recalled ?? draftRef.current;
    if (!online || !sent.text.trim() || inFlight.current || uncertain) return "rejected";
    if (relayID && (mode === "immediate" || sent.text === relayText.current)) return "rejected";
    if (!recalled && sent.text.trim().toLowerCase() === "/exit") {
      if (mode === "immediate") {
        save({ ...sent, text: "" });
        onCommand("exit");
      } else setError("채팅을 종료하려면 /exit를 보내세요.");
      return "rejected";
    }
    const parameters: Record<string, unknown> = { thread_id: chat.id, account, text: sent.text, mode,
      skills: sent.skills.map(skill => skill.id), goal: sent.goal, plan: !!sent.plan, switch_account: switchAccount };
    if (mode === "immediate" && hasOverride) {
      const model = selection.model || selectedModel?.model;
      const effort = selection.effort || inheritedEffort;
      if (model) parameters.model = model;
      if (effort) parameters.effort = effort;
    }
    inFlight.current = true;
    setAction(mode);
    setError("");
    setNotice("");
    const operation = crypto.randomUUID();
    if (!recalled) save({ ...sent, pending: operation });
    try {
      const result = await call<{ mode: "start" | "steer" | "queue" | "relay" | "goal"; relay_id?: string; account?: string;
        thread_id?: string; goal_activation_unconfirmed?: boolean; handoffError?: string;
        continuationStarted?: boolean; plan?: boolean }>("web.send", parameters, operation);
      if (result.mode === "relay" && result.relay_id) {
        relayText.current = sent.text;
        relayOperation.current = operation;
        sessionStorage.setItem(relayKey, result.relay_id);
        setRelayID(result.relay_id);
        return "accepted";
      }
      if (!recalled) settle(operation, true);
      if (result.plan) {
        setNotice(result.handoffError || "다음 턴을 계획 모드로 시작하도록 요청했습니다.");
      } else if (mode === "queue") {
        const label = result.account === "default" ? "기본 계정" : result.account;
        const queuedNotice = label ? `${label} 계정의 입력을 큐에 추가했습니다.` : "큐에 추가했습니다.";
        setNotice(result.handoffError ? queuedNotice + " " + result.handoffError :
          result.continuationStarted ? queuedNotice + " 이전 작업을 먼저 이어갑니다." :
          hasOverride ? queuedNotice + " 이 입력은 채팅의 실행 설정으로 처리됩니다." : queuedNotice);
      } else if (result.mode === "queue") {
        setNotice("새 작업 요청을 전달했습니다.");
      } else if (result.mode === "goal") {
        setNotice("채팅 목표를 시작했습니다.");
      } else if (result.goal_activation_unconfirmed) {
        setNotice("입력을 전송했습니다. 상단에서 목표 상태를 확인해 주세요.");
      } else {
        setNotice(result.mode === "steer" ? "현재 작업에 전달했습니다." : "작업을 시작했습니다.");
      }
      if (mounted.current) onSent(result.thread_id);
      return "accepted";
    } catch (e) {
      const message = (e as Error).message;
      setError(message);
      if (mutationUnconfirmed(message)) {
        return "unknown";
      }
      if (!recalled) settle(operation, false);
      return "rejected";
    } finally {
      inFlight.current = false;
      setAction(undefined);
    }
  }

  async function stop() {
    if (!online || !turn || inFlight.current) return;
    inFlight.current = true;
    setAction("stop");
    setError("");
    setNotice("");
    try {
      const result = await call<{ interrupted: boolean; queue_pause?: string }>("web.interrupt", {
        thread_id: chat.id, account: history?.executionAccount || account, turn_id: turn,
      }, crypto.randomUUID());
      if (result.queue_pause && result.interrupted) setNotice("현재 작업을 중지했습니다.");
    } catch (e) {
      setError((e as Error).message);
    } finally {
      inFlight.current = false;
      setAction(undefined);
      if (mounted.current) onSent();
    }
  }

  return <div className="composer-wrap">
    <ChatQueue thread={chat.id} account={account} items={queue} pause={history?.queuePause} busy={busy || !online} sendDisabled={uncertain}
      onChanged={onSent} onRequeue={input => send("queue", input)} />
    {external && history?.webOnly && <p className="owner-note">{switchAccount
      ? "전송 시 이 채팅의 실행을 선택한 계정으로 인계합니다."
      : "다른 앱이 이 채팅을 열고 있습니다. 이 창에서 이어가려면 실행 계정을 선택해 주세요."}</p>}
    {external && !history?.webOnly && delivery?.state !== "unavailable" && <p className="owner-note">{guiRelay ? "GUI 채팅에 해당 창의 계정으로 전달합니다." : "이 채팅을 열어 둔 창의 계정으로 전달합니다."}</p>}
    {pendingAccountChange && <p className="owner-note">{active && !draft.plan ? "보내면 현재 작업을 중단하고 새 요청을 시작합니다. 큐에 추가하면 새 계정에서 현재 작업을 먼저 이어갑니다." : "다음 입력부터 선택한 계정으로 전환합니다."}</p>}
    {draining && <p className="owner-note" role="status">기존 작업과 대기 입력이 끝나면 변경된 웹 계정이 자동 적용됩니다.</p>}
    {error && <p role="alert" className="inline-error">{error}</p>}
    {uncertain && <p className="inline-error">대화나 큐에서 이전 전송 결과를 확인하세요. <button type="button"
      className="text-button" onClick={() => save({ ...draft, pending: undefined })}>초안 계속 편집</button></p>}
    <div className="composer">
      <PromptEditor thread={chat.id} account={account} draft={draft} disabled={busy}
        onChange={next => { if (!inFlight.current) save(next); }} suggestion={active || relayID ? undefined : suggestion}
        onCommand={onCommand}
        placeholder={active ? "현재 작업에 개입하거나 다음 요청을 큐에 추가하세요" : "Codex에게 요청하세요"}
        onKeyDown={event => {
          const mode = composerAction(event.nativeEvent, queueModifier);
          if (mode) {
            event.preventDefault();
            if (!event.repeat) void send(mode);
          }
        }} />
      <div className="composer-bottom">
        <small>Enter 전송·개입 · {queueShortcut} 큐 · Shift+Enter 줄바꿈</small>
        <div className="composer-actions">
          {active && !external && <button className="stop-button" onClick={() => void stop()} disabled={busy || !online} aria-label="현재 턴 중지">■</button>}
          <button aria-keyshortcuts={`${queueModifier}+Enter`} title={queueShortcut} aria-busy={action === "queue"}
            onClick={() => void send("queue")} disabled={busy || !online || uncertain || !draft.text.trim() || relayingDraft}>
            {action === "queue" ? "큐에 추가 중…" : "큐에 추가"}
          </button>
          <button className="primary" aria-busy={action === "immediate" || !!relayID}
            onClick={() => void send("immediate")} disabled={busy || !online || !!relayID || uncertain || !draft.text.trim()}>
            {action === "immediate" || relayID ? "전달 중…" : "보내기"}
          </button>
        </div>
      </div>
    </div>
    {(relayID || notice) && <p role="status" className="delivery-notice">{relayID ? "GUI 채팅에 전달하고 있습니다…" : notice}</p>}
  </div>;
}
