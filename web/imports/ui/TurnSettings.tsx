import React, { useEffect, useState } from "react";
import { effortLabel, inheritTurnSelection, resolveTurnSelection, type ChatModel, type ChatThread, type TurnSelection } from "../contracts/web";
import { shortcutFromEvent, shortcutLabel, type ChatShortcuts } from "./chat-shortcuts";

export function TurnSettings({ chat, models, loading, modelError, retryModels, selection, onSelectionChange, shortcuts, shortcutsEnabled, deferred }: {
  chat: ChatThread;
  models: ChatModel[];
  loading: boolean;
  modelError?: string;
  retryModels: () => void;
  selection: TurnSelection;
  onSelectionChange: (selection: TurnSelection) => void;
  shortcuts: ChatShortcuts;
  shortcutsEnabled: boolean;
  deferred: boolean;
}) {
  const [error, setError] = useState("");
  const { chatModel, selectedModel, efforts, changedModel, inheritedEffort } = resolveTurnSelection(chat, models, selection);
  const inheritedModel = chatModel?.displayName || chatModel?.model || chat.model;

  function pickModel(model: string) {
    const option = model ? models.find(entry => entry.model === model) : chatModel;
    if (model && !option) {
      setError(loading ? "모델 목록을 불러오는 중입니다." : "이 계정에서 선택할 수 없는 모델입니다.");
      return;
    }
    onSelectionChange({ model, effort: option?.efforts.includes(selection.effort) ? selection.effort : "" });
    setError("");
  }

  function pickEffort(effort: string) {
    if (effort && !efforts.includes(effort)) {
      setError(loading ? "모델 목록을 불러오는 중입니다." : "선택한 모델에서 사용할 수 없는 effort입니다.");
      return;
    }
    onSelectionChange({ ...selection, effort });
    setError("");
  }

  useEffect(() => {
    if (!shortcutsEnabled) return;
    const keydown = (event: KeyboardEvent) => {
      if (event.defaultPrevented) return;
      const shortcut = shortcutFromEvent(event);
      const action = shortcut ? shortcuts[shortcut] : undefined;
      if (!action) return;
      event.preventDefault();
      if (action.kind === "model") pickModel(action.value);
      else if (action.kind === "effort") pickEffort(action.value);
      else {
        onSelectionChange(inheritTurnSelection);
        setError("");
      }
    };
    window.addEventListener("keydown", keydown, true);
    return () => window.removeEventListener("keydown", keydown, true);
  });

  function bindingHint(kind: "model" | "effort", value: string) {
    const keys = Object.entries(shortcuts).filter(([, action]) => action.kind === kind && action.value === value);
    return keys.length ? ` · ${keys.map(([shortcut]) => shortcutLabel(shortcut)).join(", ")}` : "";
  }

  return <>
    <select aria-label="모델" value={selection.model} disabled={!models.length} onChange={event => pickModel(event.target.value)}>
      <option value="">{loading ? "불러오는 중…" : inheritedModel ? `${inheritedModel} (채팅 설정)` : "채팅 설정"}</option>
      {selection.model && !selectedModel && <option value={selection.model}>{selection.model} (선택 불가)</option>}
      {models.map(model => <option key={model.model} value={model.model}>
        {model.displayName || model.model}{bindingHint("model", model.model)}
      </option>)}
    </select>
    <select aria-label="Reasoning effort" value={selection.effort} disabled={!efforts.length} onChange={event => pickEffort(event.target.value)}>
      <option value="">{inheritedEffort ? `${effortLabel(inheritedEffort)} (기본값)` : changedModel ? "모델 기본값" : "채팅 설정"}</option>
      {selection.effort && !efforts.includes(selection.effort) && <option value={selection.effort}>{effortLabel(selection.effort)} (선택 불가)</option>}
      {efforts.map(effort => <option key={effort} value={effort}>{effortLabel(effort)}{bindingHint("effort", effort)}</option>)}
    </select>
    {error && <p className="turn-settings-note inline-error" role="alert">{error}</p>}
    {modelError && <p className="turn-settings-note" role="status">모델 목록을 불러오지 못했습니다. <button type="button" className="text-button" onClick={retryModels}>다시 시도</button></p>}
    {deferred && (selection.model || selection.effort) && <p className="turn-settings-note">선택한 모델과 effort는 다음 새 턴에 적용됩니다.</p>}
  </>;
}
