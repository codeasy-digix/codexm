import React, { useRef, useState } from "react";
import { effortLabel, type ChatModel } from "../contracts/web";
import { chatShortcutStorageKey, defaultChatShortcuts, shortcutFromEvent, shortcutLabel, type ChatShortcuts, type ShortcutAction } from "./chat-shortcuts";

type DraftBinding = { id: number; shortcut: string; action?: ShortcutAction };

function makeDraft(bindings: ChatShortcuts): DraftBinding[] {
  return Object.entries(bindings).map(([shortcut, action], id) => ({ id, shortcut, action }));
}

function optionValue(action?: ShortcutAction) {
  if (!action) return "";
  return action.kind === "reset" ? "reset" : `${action.kind}:${action.value}`;
}

export function ShortcutSettings({ bindings, models, modelsLoading, modelError, retryModels, onSave, onClose }: {
  bindings: ChatShortcuts;
  models: ChatModel[];
  modelsLoading: boolean;
  modelError?: string;
  retryModels: () => void;
  onSave: (bindings: ChatShortcuts) => void;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState(() => makeDraft(bindings));
  const nextId = useRef(draft.length);
  const [error, setError] = useState("");
  const efforts = [...new Set(["max", "xhigh", "high", ...models.flatMap(model => model.efforts)])];

  function update(id: number, change: Partial<DraftBinding>) {
    setDraft(previous => previous.map(binding => binding.id === id ? { ...binding, ...change } : binding));
    setError("");
  }

  function record(event: React.KeyboardEvent<HTMLInputElement>, id: number) {
    const key = event.nativeEvent;
    if (key.isComposing || key.keyCode === 229 || key.repeat || key.getModifierState("AltGraph") ||
        ["Tab", "Escape", "Meta", "Control", "Alt", "Shift"].includes(key.key)) return;
    event.preventDefault();
    const shortcut = shortcutFromEvent(key);
    if (!shortcut) {
      setError("⌘, Ctrl, Alt 중 하나와 다른 키를 함께 누르세요. Enter, Tab, Esc는 기본 동작에 사용됩니다.");
      return;
    }
    if (draft.some(binding => binding.id !== id && binding.shortcut === shortcut)) {
      setError(`${shortcutLabel(shortcut)} 조합은 이미 할당되어 있습니다.`);
      return;
    }
    update(id, { shortcut });
  }

  function save(event: React.FormEvent) {
    event.preventDefault();
    const next: ChatShortcuts = {};
    for (const { shortcut, action } of draft) {
      if (!shortcut || !action) {
        setError("각 단축키의 키 조합과 동작을 선택해 주세요.");
        return;
      }
      next[shortcut] = action;
    }
    try {
      localStorage.setItem(chatShortcutStorageKey, JSON.stringify(next));
      onSave(next);
    } catch {
      setError("이 브라우저에 단축키 설정을 저장할 수 없습니다.");
    }
  }

  return <form className="shortcut-settings" onSubmit={save}>
    <p className="muted">키 조합 칸에서 원하는 키를 누르고 동작을 선택하세요. 브라우저나 운영체제가 먼저 처리하는 키는 다른 조합을 사용하세요.</p>
    {modelsLoading && <p className="muted" role="status">모델 목록을 불러오는 중입니다.</p>}
    {modelError && <p className="muted" role="status">모델 목록을 불러오지 못했습니다. <button type="button" className="text-button" onClick={retryModels}>다시 시도</button></p>}
    <div className="shortcut-grid">
      {draft.map(({ id, shortcut, action: current }, index) => {
        const unavailable = current?.kind === "model" && !models.some(model => model.model === current.value);
        return <div className="shortcut-row" key={id}>
          <input aria-label={`단축키 ${index + 1} 키 조합`} readOnly placeholder="키 조합 입력"
            value={shortcut && shortcutLabel(shortcut)} onKeyDown={event => record(event, id)} />
          <select aria-label={`단축키 ${index + 1} 할당`} value={optionValue(current)} onChange={event => {
            const value = event.target.value;
            const separator = value.indexOf(":");
            const action: ShortcutAction | undefined = value === "reset" ? { kind: "reset" } :
              separator < 0 ? undefined : { kind: value.startsWith("model:") ? "model" : "effort", value: value.slice(separator + 1) };
            update(id, { action });
          }}>
            <option value="">대상 선택</option>
            <option value="reset">채팅 기본 모델·effort로 복원</option>
            {unavailable && <option value={optionValue(current)}>{current.value} (현재 목록에 없음)</option>}
            <optgroup label="모델">
              {models.map(model => <option key={model.model} value={`model:${model.model}`}>{model.displayName || model.model}</option>)}
            </optgroup>
            <optgroup label="Reasoning effort">
              {efforts.map(effort => <option key={effort} value={`effort:${effort}`}>{effortLabel(effort)}</option>)}
              {current?.kind === "effort" && !efforts.includes(current.value) &&
                <option value={optionValue(current)}>{effortLabel(current.value)}</option>}
            </optgroup>
          </select>
          <button type="button" aria-label={`단축키 ${index + 1} 삭제`} onClick={() => {
            setDraft(previous => previous.filter(binding => binding.id !== id));
            setError("");
          }}>×</button>
        </div>;
      })}
    </div>
    <button className="text-button shortcut-add" type="button" onClick={() => {
      const binding = { id: nextId.current++, shortcut: "" };
      setDraft(previous => [...previous, binding]);
      setError("");
    }}>＋ 단축키 추가</button>
    {error && <p role="alert" className="inline-error">{error}</p>}
    <div className="button-row">
      <button className="text-button" type="button" onClick={() => {
        const defaults = makeDraft(defaultChatShortcuts);
        setDraft(defaults);
        nextId.current = defaults.length;
        setError("");
      }}>기본값 복원</button>
      <button type="button" onClick={onClose}>취소</button>
      <button className="primary" type="submit">저장</button>
    </div>
  </form>;
}
