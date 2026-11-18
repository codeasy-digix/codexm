import React, { useState } from "react";
import { defaultChatShortcuts, effortLabel, shortcutDigits, type ChatModel, type ChatShortcuts, type ShortcutAction } from "../contracts/web";

const storageKey = "codexm.chat-shortcuts.v1";

export function readChatShortcuts(): ChatShortcuts {
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey) || "null");
    if (!saved || typeof saved !== "object" || Array.isArray(saved)) return defaultChatShortcuts;
    const bindings: ChatShortcuts = {};
    for (const digit of shortcutDigits) {
      const action = saved[digit];
      if ((action?.kind === "model" || action?.kind === "effort") &&
          typeof action.value === "string" && action.value.length > 0 && action.value.length <= 200) {
        bindings[digit] = { kind: action.kind, value: action.value };
      }
    }
    return bindings;
  } catch {
    return defaultChatShortcuts;
  }
}

function optionValue(action?: ShortcutAction) {
  return action ? `${action.kind}:${action.value}` : "";
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
  const [draft, setDraft] = useState(bindings);
  const [error, setError] = useState("");
  const efforts = [...new Set(["max", "xhigh", "high", ...models.flatMap(model => model.efforts)])];

  function save(event: React.FormEvent) {
    event.preventDefault();
    try {
      localStorage.setItem(storageKey, JSON.stringify(draft));
      onSave(draft);
    } catch {
      setError("이 브라우저에 단축키 설정을 저장할 수 없습니다.");
    }
  }

  return <form className="shortcut-settings" onSubmit={save}>
    <p className="muted">채팅에서 사용할 단축키를 설정합니다. 브라우저가 ⌘ 숫자를 탭 전환에 쓰면 Ctrl+Shift+숫자를 사용하세요.</p>
    {modelsLoading && <p className="muted" role="status">모델 목록을 불러오는 중입니다.</p>}
    {modelError && <p className="muted" role="status">모델 목록을 불러오지 못했습니다. <button type="button" className="text-button" onClick={retryModels}>다시 시도</button></p>}
    <div className="shortcut-grid">
      {shortcutDigits.map(digit => {
        const current = draft[digit];
        const unavailable = current?.kind === "model" && !models.some(model => model.model === current.value);
        return <label key={digit}>
          <kbd>⌘{digit}</kbd>
          <select aria-label={`⌘${digit} 할당`} value={optionValue(current)} onChange={event => {
            const value = event.target.value;
            const separator = value.indexOf(":");
            setDraft(previous => ({
              ...previous,
              [digit]: separator < 0 ? undefined : { kind: value.startsWith("model:") ? "model" : "effort", value: value.slice(separator + 1) },
            }));
          }}>
            <option value="">미할당</option>
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
        </label>;
      })}
    </div>
    {error && <p role="alert" className="inline-error">{error}</p>}
    <div className="button-row">
      <button className="text-button" type="button" onClick={() => setDraft(defaultChatShortcuts)}>기본값 복원</button>
      <button type="button" onClick={onClose}>취소</button>
      <button className="primary" type="submit">저장</button>
    </div>
  </form>;
}
