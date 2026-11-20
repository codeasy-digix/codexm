import React, { useEffect, useId, useRef, useState } from "react";
import { call } from "../../client/data";
import type { ChatCommand, ChatSkill } from "../contracts/web";
import { promptMenuRange, type PromptDraft } from "./prompt-draft";

type Entry = { kind: "command"; id: string; name: ChatCommand | "goal" | "plan"; description: string }
  | (ChatSkill & { kind: "skill" });
const commands: Entry[] = [
  { kind: "command", id: "goal", name: "goal", description: "채팅 목표 시작" },
  { kind: "command", id: "plan", name: "plan", description: "다음 턴을 계획 모드로 시작" },
  { kind: "command", id: "status", name: "status", description: "채팅과 실행 상태 보기" },
  { kind: "command", id: "rename", name: "rename", description: "채팅 이름 변경" },
  { kind: "command", id: "review", name: "review", description: "Git 변경사항 리뷰" },
  { kind: "command", id: "compact", name: "compact", description: "채팅 컨텍스트 압축" },
];

export function PromptEditor({ thread, account, draft, disabled, placeholder, suggestion, onChange, onCommand, onKeyDown }: {
  thread: string;
  account: string;
  draft: PromptDraft;
  disabled: boolean;
  placeholder: string;
  suggestion?: string;
  onChange: (draft: PromptDraft) => void;
  onCommand: (command: ChatCommand) => void;
  onKeyDown: (event: React.KeyboardEvent<HTMLTextAreaElement>) => void;
}) {
  const field = useRef<HTMLTextAreaElement>(null);
  const menuID = useId();
  const [range, setRange] = useState<ReturnType<typeof promptMenuRange>>();
  const [cursor, setCursor] = useState(0);
  const [requested, setRequested] = useState(false);
  const [available, setAvailable] = useState<ChatSkill[]>([]);
  const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
  const composing = useRef(false);

  useEffect(() => { if (disabled) setRange(undefined); }, [disabled]);

  useEffect(() => {
    if (!requested) return;
    let closed = false;
    setAvailable([]);
    setStatus("loading");
    void call<ChatSkill[]>("web.skills", { thread_id: thread, account }).then(skills => {
      if (!closed) { setAvailable(skills); setStatus("ready"); }
    }).catch(() => { if (!closed) setStatus("error"); });
    return () => { closed = true; };
  }, [thread, account, requested]);

  const showSkills = !!range && (range.prefix === "$" || range.search !== "");
  const entries: Entry[] = !range ? [] : [
    ...(range.prefix === "/" ? commands.filter(item => item.name.includes(range.search) &&
      !(item.name === "goal" && draft.goal) && !(item.name === "plan" && draft.plan)) : []),
    ...(showSkills ? available.filter(skill => skill.name.toLowerCase().includes(range.search) &&
      !draft.skills.some(selected => selected.id === skill.id)).map(skill => ({ ...skill, kind: "skill" as const })) : []),
  ];
  const position = Math.min(cursor, Math.max(0, entries.length - 1));
  const predicted = !disabled && !draft.text && !draft.pending ? suggestion : undefined;

  function updateRange(text: string, caret: number) {
    const next = composing.current ? undefined : promptMenuRange(text, caret);
    setCursor(0);
    setRange(next);
    if (next && (next.prefix === "$" || next.search)) setRequested(true);
  }

  function pick(item: Entry) {
    if (disabled || !range) return;
    const { start, end } = range;
    const next = { ...draft, text: draft.text.slice(0, start) + draft.text.slice(end), pending: undefined };
    setRange(undefined);
    if (item.kind === "skill") next.skills = [...draft.skills, { id: item.id, name: item.name }].slice(0, 16);
    else if (item.name === "goal" || item.name === "plan") {
      next.goal = item.name === "goal";
      next.plan = item.name === "plan";
    } else {
      onChange(next);
      onCommand(item.name);
      return;
    }
    onChange(next);
    requestAnimationFrame(() => {
      field.current?.focus();
      field.current?.setSelectionRange(start, start);
    });
  }

  function keyDown(event: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (disabled || composing.current || event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229) return;
    if (range && event.key === "Escape") {
      event.preventDefault();
      setRange(undefined);
      return;
    }
    if (range && entries.length && !event.metaKey && !event.ctrlKey && !event.altKey && !event.shiftKey) {
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        const direction = event.key === "ArrowDown" ? 1 : -1;
        const next = (position + direction + entries.length) % entries.length;
        setCursor(next);
        document.getElementById(`${menuID}-${next}`)?.scrollIntoView({ block: "nearest" });
        return;
      }
      if (event.key === "Enter" || event.key === "Tab") {
        event.preventDefault();
        if (!event.repeat) pick(entries[position]);
        return;
      }
    }
    if (predicted && event.key === "Tab" && !event.shiftKey && !event.metaKey && !event.ctrlKey && !event.altKey) {
      event.preventDefault();
      if (!event.repeat) onChange({ ...draft, text: predicted });
      return;
    }
    onKeyDown(event);
  }

  return <div className="prompt-editor" aria-busy={disabled}>
    {(draft.goal || draft.plan || draft.skills.length > 0) && <div className="prompt-chips" aria-label="적용할 명령과 스킬">
      {draft.goal && <button type="button" className="prompt-chip" aria-label="목표 지정 제거" disabled={disabled} onClick={() =>
        onChange({ ...draft, goal: false, pending: undefined })}>/goal <span aria-hidden="true">×</span></button>}
      {draft.plan && <button type="button" className="prompt-chip" aria-label="계획 모드 제거" disabled={disabled} onClick={() =>
        onChange({ ...draft, plan: false, pending: undefined })}>/plan <span aria-hidden="true">×</span></button>}
      {draft.skills.map(skill => <button key={skill.id} type="button" className="prompt-chip" aria-label={`${skill.name} 스킬 제거`} disabled={disabled}
        onClick={() => onChange({ ...draft, skills: draft.skills.filter(item => item.id !== skill.id), pending: undefined })}>
        {skill.name} <span aria-hidden="true">×</span>
      </button>)}
    </div>}
    <textarea ref={field} aria-label="메시지" value={draft.text} placeholder={predicted ?? placeholder} disabled={disabled}
      aria-keyshortcuts={predicted ? "Tab" : undefined}
      aria-description={predicted ? "Tab을 눌러 Codex의 다음 입력 제안을 작성창에 넣습니다." : undefined}
      aria-autocomplete="list" aria-controls={!disabled && range ? menuID : undefined}
      aria-activedescendant={!disabled && range && entries.length ? `${menuID}-${position}` : undefined}
      rows={3} enterKeyHint="send" onBlur={() => setRange(undefined)}
      onCompositionStart={() => { composing.current = true; setRange(undefined); }}
      onCompositionEnd={event => {
        composing.current = false;
        updateRange(event.currentTarget.value, event.currentTarget.selectionStart);
      }}
      onChange={event => {
        const text = event.target.value;
        onChange({ ...draft, text, pending: undefined });
        updateRange(text, event.target.selectionStart);
      }}
      onClick={event => updateRange(event.currentTarget.value, event.currentTarget.selectionStart)}
      onKeyUp={event => {
        if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) updateRange(event.currentTarget.value, event.currentTarget.selectionStart);
      }}
      onKeyDown={keyDown} />
    {!disabled && range && <div className="prompt-menu" id={menuID} role="listbox" aria-label={range.prefix === "$" ? "스킬" : "명령과 스킬"}>
      {entries.map((item, index) => <button type="button" id={`${menuID}-${index}`} role="option"
        key={item.kind + item.id} tabIndex={-1} aria-selected={index === position} onPointerDown={event => event.preventDefault()}
        onClick={() => pick(item)}>
        <strong>{item.kind === "skill" ? "$" : "/"}{item.name}</strong>{item.description && <small>{item.description}</small>}
      </button>)}
      {showSkills && status === "loading" && !entries.length && <p role="status">스킬 불러오는 중…</p>}
      {showSkills && status === "error" && !entries.length && <p role="status">스킬 목록을 불러올 수 없습니다.</p>}
      {(!showSkills || status === "ready") && !entries.length && <p>일치하는 명령이나 스킬이 없습니다.</p>}
    </div>}
  </div>;
}
