import React, { useEffect, useRef, useState } from "react";
import { call, mutationUnconfirmed } from "../../client/data";
import { ChatQueue, type SubmissionOutcome } from "./ChatQueue";
import { Modal } from "./Modal";
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
  observedTurn?: string;
  models: ChatModel[];
  selection: TurnSelection;
  onCommand: (command: ChatCommand) => void;
  onSent: (id?: string) => void;
};

type SendMode = "immediate" | "relay" | "queue";
type Handoff = { mode: "relay" | "immediate"; turn?: string };

export function Composer({ chat, online, account, history, liveTurn, observedTurn, models, selection, onCommand, onSent, pendingAccountChange, switchAccount, suggestion }: Props) {
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
  const [action, setAction] = useState<SendMode | "stop">();
  const busy = action !== undefined;
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const inFlight = useRef(false);
  const [handoff, setHandoff] = useState<Handoff>();
  const relayKey = `codexm.guiRelay.${chat.id}`;
  const [relayID, setRelayID] = useState(() => sessionStorage.getItem(relayKey) || "");
  const [relayHandoff, setRelayHandoff] = useState(() => sessionStorage.getItem(`${relayKey}.handoff`) || "");
  const relayText = useRef(relayID ? draft.text : "");
  const relayOperation = useRef(relayID ? draft.pending : undefined);
  const refresh = useRef(onSent);
  refresh.current = onSent;
  const external = history?.thread.externalOwner;
  const delivery = history?.delivery;
  const guiOwned = external && (delivery?.gui || delivery?.state === "relay");
  const canRelayHere = guiOwned && delivery?.state === "relay";
  const canTransfer = !!history?.guiHandoffAvailable;
  const canRelay = canRelayHere || !guiOwned && canTransfer;
  const canDirect = !guiOwned && (!external || switchAccount || delivery?.state === "idle" || delivery?.state === "ready");
  const enterMode = guiOwned && !switchAccount ? "relay" : "immediate";
  const plainMessage = !draft.goal && !draft.plan && draft.skills.length === 0;
  const relayingDraft = !!relayID && draft.text === relayText.current;
  const uncertain = !!draft.pending && !busy && !relayingDraft;
  const active = external ? delivery?.state === "ready" || delivery?.state === "waiting" || (guiOwned && !!delivery?.turnId)
    : liveTurn ? liveTurn.status === "inProgress" : history?.thread.status.type === "active";
  const turn = external ? observedTurn || delivery?.turnId : active
    ? liveTurn?.id ?? history?.turns.slice().reverse().find(t => t.status === "inProgress")?.id : undefined;
  const draining = !!history?.loginDraining;
  const queue = history?.queue || [];
  const { selectedModel, inheritedEffort } = resolveTurnSelection(chat, models, selection);
  const hasOverride = !!(selection.model || selection.effort);
  const sendDisabled = busy || !online || uncertain || !draft.text.trim();

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
      let detail = "";
      try {
        const result = await call<{ status: string; detail?: string }>("web.guiRelayStatus", { thread_id: chat.id, request_id: relayID });
        status = result.status;
        detail = result.detail || "";
      } catch { /* Retain the original input if the delivery result cannot be verified. */ }
      if (closed) return;
      if (status === "pending") {
        timer = setTimeout(() => void check(), 1500);
        return;
      }
      sessionStorage.removeItem(relayKey);
      sessionStorage.removeItem(`${relayKey}.handoff`);
      setRelayID("");
      setRelayHandoff("");
      const operation = relayOperation.current;
      if (status === "accepted") {
        if (operation) settle(operation, true);
        setNotice(relayHandoff === "web" ? "CodexM에서 채팅을 이어서 실행했습니다." : "GUI 채팅에 전달했습니다.");
        refresh.current();
      } else if (status === "expired") {
        if (operation) settle(operation, false);
        setError("중계 시간이 만료됐습니다. 입력은 작성창에 남아 있습니다.");
      } else if (status === "rejected") {
        if (operation) settle(operation, false);
        setError(detail || "인계할 채팅의 상태가 바뀌었습니다. 입력은 작성창에 남아 있습니다.");
      } else {
        setError(detail || "전달 결과를 확인하지 못했습니다. 채팅과 대기큐를 확인해 주세요.");
      }
    }
    void check();
    return () => { closed = true; clearTimeout(timer); };
  }, [chat.id, relayID, relayKey, relayHandoff, online]);

  async function send(mode: SendMode, recalled?: PromptDraft, transfer?: Handoff): Promise<SubmissionOutcome> {
    const sent = recalled ?? draftRef.current;
    if (!online || !sent.text.trim() || inFlight.current || uncertain) return "rejected";
    if (relayID && (mode !== "queue" || sent.text === relayText.current)) return "rejected";
    if (!recalled && sent.text.trim().toLowerCase() === "/exit") {
      if (mode !== "queue") {
        save({ ...sent, text: "" });
        onCommand("exit");
      } else setError("채팅을 종료하려면 /exit를 보내세요.");
      return "rejected";
    }
    if (mode === "relay" && (!(transfer ? canTransfer : canRelayHere) || !plainMessage)) {
      setError(!canRelay ? "이 채팅의 GUI 중계를 사용할 수 없습니다." : "GUI 중계로 보낼 일반 메시지를 입력해 주세요.");
      return "rejected";
    }
    if (mode === "immediate" && !(transfer ? canTransfer : canDirect)) {
      setError(guiOwned && canTransfer ? "CodexM으로 인계하려면 즉시 실행 버튼을 눌러 주세요." :
        guiOwned ? "GUI 중계를 선택하거나 큐에 추가해 주세요." : "이 채팅을 열어 둔 CLI에 바로 전달할 수 없습니다.");
      return "rejected";
    }
    const parameters: Record<string, unknown> = { thread_id: chat.id, account, text: sent.text, mode,
      skills: sent.skills.map(skill => skill.id), goal: sent.goal, plan: !!sent.plan,
      switch_account: mode !== "relay" && switchAccount };
    if (transfer) {
      parameters.gui_handoff = true;
      if (transfer.turn) parameters.turn_id = transfer.turn;
    }
    if (mode === "immediate" && hasOverride && (!external || switchAccount || transfer)) {
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
        thread_id?: string; goal_activation_unconfirmed?: boolean; handoffError?: string; gui?: boolean;
        continuationStarted?: boolean; plan?: boolean; handoff?: "gui" | "web" }>("web.send", parameters, operation);
      if (result.mode === "relay" && result.relay_id) {
        relayText.current = sent.text;
        relayOperation.current = operation;
        sessionStorage.setItem(relayKey, result.relay_id);
        if (result.handoff) sessionStorage.setItem(`${relayKey}.handoff`, result.handoff);
        setRelayHandoff(result.handoff || "");
        setRelayID(result.relay_id);
        return "accepted";
      }
      if (!recalled) settle(operation, true);
      if (result.plan) {
        setNotice(result.handoffError || "다음 턴을 계획 모드로 시작하도록 요청했습니다.");
      } else if (mode === "queue") {
        const label = result.account === "default" ? "기본 계정" : result.account;
        const queuedNotice = result.gui ? "GUI에서 실행할 입력을 큐에 추가했습니다." :
          label ? `${label} 계정의 입력을 큐에 추가했습니다.` : "큐에 추가했습니다.";
        setNotice(result.handoffError ? queuedNotice + " " + result.handoffError :
          result.continuationStarted ? queuedNotice + " 이전 작업을 먼저 이어갑니다." :
          hasOverride ? queuedNotice + " 이 입력은 채팅의 실행 설정으로 처리됩니다." : queuedNotice);
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

  function chooseDelivery(mode: "relay" | "immediate") {
    const transfer = mode === "relay" ? !guiOwned : guiOwned;
    if (transfer && draftRef.current.text.trim().toLowerCase() !== "/exit") {
      setHandoff({ mode, turn: observedTurn || turn });
    } else {
      void send(mode);
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
    {guiOwned && <p className="owner-note">{canRelayHere
      ? (switchAccount ? "GUI 중계는 데스크톱 계정으로 전달합니다. 선택한 계정은 큐에 추가할 때 사용합니다." : "GUI 중계는 데스크톱의 계정과 모델로 전달합니다.")
      : "데스크톱이 이 채팅을 열고 있습니다. 큐에 입력을 추가할 수 있습니다."}</p>}
    {external && !guiOwned && <p className="owner-note">{switchAccount && history?.webOnly
      ? "전송 시 이 채팅의 실행을 선택한 계정으로 인계합니다."
      : delivery?.state === "unavailable" ? "현재 CLI의 실행 연결을 확인할 수 없습니다."
      : "즉시 실행은 이 채팅을 열어 둔 CLI의 계정을 사용합니다."}</p>}
    {pendingAccountChange && <p className="owner-note">{active && !draft.plan ? "즉시 실행은 현재 작업을 중단하고 새 요청을 시작합니다. 큐에 추가하면 새 계정에서 현재 작업을 먼저 이어갑니다." : "다음 입력부터 선택한 계정으로 전환합니다."}</p>}
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
            if (!event.repeat) void send(mode === "queue" ? mode : enterMode);
          }
        }} />
      <div className="composer-bottom">
        <small>{(enterMode === "relay" ? canRelayHere : canDirect) && (enterMode === "relay" ? "Enter GUI 중계 · " : "Enter 즉시 실행 · ")}{queueShortcut} 큐 · Shift+Enter 줄바꿈</small>
        <div className="composer-actions">
          {active && !external && <button className="stop-button" onClick={() => void stop()} disabled={busy || !online} aria-label="현재 턴 중지">■</button>}
          <button aria-keyshortcuts={`${queueModifier}+Enter`} title={queueShortcut} aria-busy={action === "queue"}
            onClick={() => void send("queue")} disabled={sendDisabled || relayingDraft}>
            {action === "queue" ? "큐에 추가 중…" : "큐에 추가"}
          </button>
          <button className={guiOwned && canRelayHere ? "primary" : undefined} aria-busy={action === "relay" || (!!relayID && relayHandoff !== "web")}
            aria-keyshortcuts={enterMode === "relay" && canRelayHere ? "Enter" : undefined}
            onClick={() => chooseDelivery("relay")} disabled={sendDisabled || !!relayID || !canRelay || !plainMessage}>
            {action === "relay" || (relayID && relayHandoff !== "web") ? "중계 중…" : "GUI 중계"}
          </button>
          <button className={canDirect ? "primary" : undefined} aria-busy={action === "immediate" || relayHandoff === "web"}
            aria-keyshortcuts={canDirect ? "Enter" : undefined}
            onClick={() => chooseDelivery("immediate")} disabled={sendDisabled || !!relayID || !(canDirect || guiOwned && canTransfer)}>
            {action === "immediate" || relayHandoff === "web" ? "전달 중…" : "즉시 실행"}
          </button>
        </div>
      </div>
    </div>
    {(relayID || notice) && <p role="status" className="delivery-notice">{relayID ?
      (relayHandoff === "web" ? "CodexM으로 채팅을 인계하고 있습니다…" : "GUI 채팅에 전달하고 있습니다…") : notice}</p>}
    {handoff && <Modal title={handoff.mode === "relay" ? "데스크톱으로 채팅 인계" : "CodexM으로 채팅 인계"} onClose={() => setHandoff(undefined)}>
      {handoff.turn && <p>이 채팅의 현재 작업을 중단한 뒤 작성한 메시지를 보냅니다.</p>}
      <p>같은 채팅에서 이어갑니다. 대기큐의 내용과 순서를 보존합니다.</p>
      <div className="button-row">
        <button type="button" autoFocus onClick={() => setHandoff(undefined)}>취소</button>
        <button type="button" className="primary" onClick={() => {
          setHandoff(undefined);
          void send(handoff.mode, undefined, handoff);
        }}>인계하고 실행</button>
      </div>
    </Modal>}
  </div>;
}
