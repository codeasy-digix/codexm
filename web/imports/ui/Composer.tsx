import React, { useEffect, useRef, useState } from "react";
import { call } from "../../client/data";
import {
  chatSettingsShortcut, composerAction, effortLabel, shortcutDigits,
  type ChatHistory, type ChatModel, type ChatShortcuts, type ChatThread, type ShortcutAction, type TurnSelection,
} from "../contracts/web";

type Props = {
  chat: ChatThread;
  account: string;
  history?: ChatHistory;
  models: ChatModel[];
  modelsLoading: boolean;
  modelError?: string;
  retryModels: () => void;
  selection: TurnSelection;
  onSelectionChange: (selection: TurnSelection) => void;
  shortcuts: ChatShortcuts;
  shortcutsEnabled: boolean;
  onSent: () => void;
};

export function Composer({
  chat, account, history, models, modelsLoading, modelError, retryModels,
  selection, onSelectionChange, shortcuts, shortcutsEnabled, onSent,
}: Props) {
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [uncertain, setUncertain] = useState(false);
  const inFlight = useRef(false);
  const active = history?.thread.status.type === "active";
  const external = history?.thread.externalOwner;
  const turn = active ? history.turns.slice().reverse().find(t => t.status === "inProgress")?.id : undefined;
  const queue = history?.queue || [];
  const chatModel = chat.model
    ? models.find(model => model.model === chat.model)
    : models.find(model => model.isDefault);
  const selectedModel = selection.model ? models.find(model => model.model === selection.model) : chatModel;
  const efforts = selectedModel?.efforts ?? [];
  const changedModel = !!selection.model && selection.model !== chat.model;
  const inheritedEffort = !changedModel && efforts.includes(chat.reasoningEffort || "")
    ? chat.reasoningEffort
    : selectedModel?.defaultEffort;
  const hasOverride = !!(selection.model || selection.effort);

  function pickModel(model: string) {
    const option = model ? models.find(entry => entry.model === model) : chatModel;
    if (model && !option) {
      setError(modelsLoading ? "모델 목록을 불러오는 중입니다." : "이 계정에서 선택할 수 없는 모델입니다.");
      return;
    }
    onSelectionChange({ model, effort: option?.efforts.includes(selection.effort) ? selection.effort : "" });
    setError("");
  }

  function pickEffort(effort: string) {
    if (effort && !efforts.includes(effort)) {
      setError(modelsLoading ? "모델 목록을 불러오는 중입니다." : "선택한 모델에서 사용할 수 없는 effort입니다.");
      return;
    }
    onSelectionChange({ ...selection, effort });
    setError("");
  }

  // Listen while an editable chat is open. Dialogs and IME composition keep
  // their own keyboard input. The browser may reserve Command+digit globally.
  useEffect(() => {
    if (!shortcutsEnabled) return;
    const keydown = (event: KeyboardEvent) => {
      if (event.defaultPrevented) return;
      const action = chatSettingsShortcut(event, shortcuts);
      if (!action) return;
      event.preventDefault();
      if (action.kind === "model") pickModel(action.value);
      else pickEffort(action.value);
    };
    window.addEventListener("keydown", keydown, true);
    return () => window.removeEventListener("keydown", keydown, true);
  });

  function shortcutLabel(kind: ShortcutAction["kind"], value: string) {
    const keys = shortcutDigits.filter(digit => shortcuts[digit]?.kind === kind && shortcuts[digit]?.value === value);
    return keys.length ? ` · ${keys.map(key => `⌘${key}`).join(", ")}` : "";
  }

  async function send(mode: "immediate" | "queue") {
    if (!text.trim() || inFlight.current || uncertain) return;
    if (mode === "immediate" && external) {
      setError("이 채팅은 다른 GUI/CLI가 소유하고 있습니다. 원래 창에서 해제하거나 큐에 추가하세요.");
      return;
    }
    const override = mode === "immediate" && !active && hasOverride;
    if (override && (!selectedModel || (selection.effort && !efforts.includes(selection.effort)))) {
      setError("현재 계정에서 사용할 모델과 effort를 다시 선택해 주세요.");
      return;
    }
    const parameters: Record<string, string> = { thread_id: chat.id, account, text, mode };
    if (mode === "immediate" && turn) parameters.turn_id = turn;
    if (override && selectedModel) {
      parameters.model = selectedModel.model;
      const effort = selection.effort || inheritedEffort;
      if (effort && efforts.includes(effort)) parameters.effort = effort;
    }
    inFlight.current = true;
    setBusy(true);
    setError("");
    setNotice("");
    const sent = text;
    try {
      await call("web.send", parameters, crypto.randomUUID());
      setText(current => current === sent ? "" : current);
      if (mode === "queue") {
        setNotice(hasOverride ? "큐에 추가했습니다. 이 입력은 채팅의 실행 설정으로 처리됩니다." : "큐에 추가했습니다.");
      } else {
        setNotice(active ? "현재 작업에 전달했습니다." : "작업을 시작했습니다.");
      }
      onSent();
    } catch (e) {
      const message = (e as Error).message;
      setError(message);
      if (/outcome|확인하지 못|대기 시간|연결이 끊|unknown|timed out|deadline|EOF|disconnected/i.test(message)) setUncertain(true);
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

  const inheritedModel = chatModel?.displayName || chatModel?.model || chat.model;
  return <div className="composer-wrap">
    {queue.length > 0 && <details className="queue-preview">
      <summary>대기 중인 입력 {queue.length}개</summary>
      {queue.map(q => <p key={q.id}>{q.input.map(x => x.text || "").join("\n")}</p>)}
    </details>}
    {external && <p className="owner-note">다른 GUI/CLI에서 사용 중 · 큐 입력은 원래 실행 계정에서 처리됩니다.</p>}
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
      <div className="turn-settings" aria-label="다음 새 턴 설정">
        <label>모델
          <select value={selection.model} disabled={!models.length || busy} onChange={e => pickModel(e.target.value)}>
            <option value="">{modelsLoading ? "불러오는 중…" : inheritedModel ? `채팅 설정 · ${inheritedModel}` : "채팅 설정"}</option>
            {selection.model && !selectedModel && <option value={selection.model}>{selection.model} (선택 불가)</option>}
            {models.map(model => <option key={model.model} value={model.model}>
              {model.displayName || model.model}{shortcutLabel("model", model.model)}
            </option>)}
          </select>
        </label>
        <label>Reasoning effort
          <select value={selection.effort} disabled={!efforts.length || busy} onChange={e => pickEffort(e.target.value)}>
            <option value="">{changedModel ? "모델 기본값" : "채팅 설정"}{inheritedEffort ? ` · ${effortLabel(inheritedEffort)}` : ""}</option>
            {selection.effort && !efforts.includes(selection.effort) && <option value={selection.effort}>{effortLabel(selection.effort)} (선택 불가)</option>}
            {efforts.map(effort => <option key={effort} value={effort}>{effortLabel(effort)}{shortcutLabel("effort", effort)}</option>)}
          </select>
        </label>
      </div>
      {modelError && <p className="model-load-error" role="status">모델 목록을 불러오지 못했습니다. <button type="button" className="text-button" onClick={retryModels}>다시 시도</button></p>}
      {(active || external) && hasOverride && <p className="model-load-error">선택한 모델과 effort는 다음 새 턴에 적용됩니다.</p>}
      <div className="composer-bottom">
        <small>Enter 전송·개입 · ⌘Enter 큐 · Shift+Enter 줄바꿈</small>
        <div className="composer-actions">
          {active && !external && <button className="stop-button" onClick={() => void stop()} disabled={busy} aria-label="현재 턴 중지">■</button>}
          <button aria-keyshortcuts="Meta+Enter" onClick={() => void send("queue")} disabled={busy || uncertain || !text.trim()}>큐에 추가</button>
          <button className="primary" onClick={() => void send("immediate")} disabled={busy || uncertain || !text.trim() || external}>
            {busy ? "전달 중…" : active ? "즉시 개입" : "보내기"}
          </button>
        </div>
      </div>
    </div>
    {notice && <p role="status" className="delivery-notice">{notice}</p>}
  </div>;
}
