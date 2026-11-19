import React, { useId, useRef, useState } from "react";
import { call, mutationUnconfirmed, queueDraftKeyPrefix } from "../../client/data";
import type { QueuedInput } from "../contracts/web";
import type { PromptDraft } from "./prompt-draft";
import { inputText } from "./ChatMessages";

export type SubmissionOutcome = "accepted" | "rejected" | "unknown";
type RecalledDraft = PromptDraft & { kind: "recalled"; checkHistory: boolean; unconfirmed: boolean };
type QueueDraft = RecalledDraft | { kind: "editing"; id: string; original: string; text: string };

function editableText(item: QueuedInput) {
  const [part] = item.input;
  if (item.input.length === 1 && part.type === "text" &&
      typeof part.text === "string" && !part.text_elements?.length) return part.text;
}

function readRecalledDraft(key: string): RecalledDraft | undefined {
  try {
    const stored = JSON.parse(sessionStorage.getItem(key) || "null");
    if (typeof stored?.text === "string") {
      return { kind: "recalled", text: stored.text, skills: stored.skills ?? [], goal: stored.goal === true,
        checkHistory: stored.checkHistory === true, unconfirmed: stored.unconfirmed === true };
    }
  } catch { /* Storage may be disabled. The current editor still works. */ }
}

export function ChatQueue({ thread, account, items, busy, sendDisabled, onChanged, onRequeue }:{
  thread: string;
  account: string;
  items: QueuedInput[];
  busy: boolean;
  sendDisabled: boolean;
  onChanged: () => void;
  onRequeue: (draft: PromptDraft) => Promise<SubmissionOutcome>;
}) {
  const key = queueDraftKeyPrefix + thread;
  const editorID = useId();
  const [draft, setDraft] = useState<QueueDraft | undefined>(() => readRecalledDraft(key));
  const [pending, setPending] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const inFlight = useRef(false);
  const locked = busy || pending || uncertain;
  const queued = draft?.kind === "editing" ? items.find(item => item.id === draft.id) : undefined;
  const changed = draft?.kind === "editing" && (!queued || editableText(queued) !== draft.original);

  function keep(next?: QueueDraft) {
    setDraft(next);
    try {
      if (next?.kind === "recalled") sessionStorage.setItem(key, JSON.stringify(next));
      else sessionStorage.removeItem(key);
    } catch { /* Keep the in-memory draft if this browser cannot store it. */ }
  }

  async function changeQueue(id: string, original: string, text?: string) {
    if (locked || inFlight.current) return;
    inFlight.current = true;
    setPending(true);
    setError("");
    setNotice("");
    const remove = text === undefined;
    const recalled = draft?.kind === "editing" && draft.id === id ? draft.text : original;
    const item = items.find(item => item.id === id);
    const draftToRecall: RecalledDraft = { kind: "recalled", text: recalled, skills: item?.skills ?? [], goal: !!item?.goal,
      checkHistory: false, unconfirmed: true };
    // Keep the text if the tab disconnects after Codex removes the item.
    if (remove) keep(draftToRecall);
    const params = { thread_id: thread, account, queued_id: id, expected_text: original,
      ...(remove ? {} : { text }) };
    try {
      const result = await call<{ updated?: boolean; deleted?: boolean; check_history?: boolean }>(
        remove ? "web.queueCancel" : "web.queueUpdate", params, crypto.randomUUID());
      if ((remove ? result.deleted : result.updated) !== true) {
        throw new Error("큐 변경 결과를 확인하지 못했습니다.");
      }
      if (remove) {
        keep({ ...draftToRecall, checkHistory: result.check_history === true, unconfirmed: false });
        setNotice("대기열에서 뺐습니다.");
      } else {
        keep();
        setNotice("대기 메시지를 수정했습니다.");
      }
    } catch (reason) {
      const message = (reason as Error).message;
      setError(message);
      if (mutationUnconfirmed(message)) {
        setUncertain(true);
      } else if (remove) {
        keep(draft);
      }
    } finally {
      inFlight.current = false;
      setPending(false);
      onChanged();
    }
  }

  async function requeue(draft: RecalledDraft) {
    if (locked || sendDisabled || inFlight.current || draft.unconfirmed) return;
    inFlight.current = true;
    setPending(true);
    setError("");
    setNotice("");
    keep({ ...draft, unconfirmed: true });
    try {
      const result = await onRequeue(draft);
      if (result === "accepted") keep();
      if (result === "rejected") keep(draft);
    } finally {
      inFlight.current = false;
      setPending(false);
    }
  }

  return <>
    {items.length > 0 && <details className="queue-preview">
      <summary><strong>대기 중인 입력 {items.length}개</strong>
        <span className="queue-next" title={inputText(items[0].input)}>{inputText(items[0].input)}</span></summary>
      <ul className="queue-entries">
        {items.map((item, index) => {
          const original = editableText(item);
          const selected = draft?.kind === "editing" && draft.id === item.id;
          return <li key={item.id} className="queue-entry">
            <div className="queue-entry-meta">
              <span>{item.state === "sending" ? "전송 결과 확인 필요" : item.state === "blocked" || item.state === "held" ? "실행 보류" : index === 0 ? "다음 입력" : "대기 중"}</span>
              {item.account && <small>{item.account === "default" ? "기본 계정" : item.account}</small>}
              {item.goal && <small>/goal</small>}
              {item.skills?.map(skill => <small key={skill.id}>{skill.name}</small>)}
            </div>
            <p>{inputText(item.input)}</p>
            {item.detail && <small className="muted">{item.detail}</small>}
            {original !== undefined && <div className="queue-actions">
              <button type="button" disabled={locked || !!draft || item.state === "sending"} onClick={() => {
                setError("");setNotice("");
                keep({ kind: "editing", id: item.id, original, text: original });
              }}>수정</button>
              <button type="button" disabled={locked || (!!draft && (!selected || changed))}
                onClick={() => void changeQueue(item.id, original)}>대기 취소</button>
            </div>}
          </li>;
        })}
      </ul>
    </details>}
    {draft && <section className="queue-editor">
      <label htmlFor={editorID}>{draft.kind === "editing" ? "대기 메시지 수정" : draft.unconfirmed ? "보관된 입력 초안" : "큐에서 뺀 메시지"}</label>
      <textarea id={editorID} rows={3} value={draft.text} onChange={e => keep({ ...draft, text: e.target.value })}
        readOnly={pending} />
      {draft.kind === "editing" && changed && <p role="status">대기 상태가 바뀌었습니다. 적어 둔 내용은 복사할 수 있습니다.</p>}
      {draft.kind === "recalled" && draft.checkHistory && <p>이미 실행을 시작했는지 대화에서 확인한 뒤 다시 추가하세요.</p>}
      {draft.kind === "recalled" && draft.unconfirmed && !pending && <p role="status">대기열 변경 결과를 대화와 대기열에서 먼저 확인해 주세요. 초안은 복사할 수 있습니다.</p>}
      <div className="queue-actions">
        {draft.kind === "editing" ? <button type="button" className="primary"
          disabled={locked || changed || !draft.text.trim() || draft.text === draft.original}
          onClick={() => void changeQueue(draft.id, draft.original, draft.text)}>수정 저장</button>
          : <button type="button" className="primary" disabled={locked || sendDisabled || draft.unconfirmed || !draft.text.trim()}
            onClick={() => void requeue(draft)}>다시 큐에 추가</button>}
        <button type="button" disabled={pending} onClick={() => { keep();setNotice(""); }}>
          {draft.kind === "editing" ? "닫기" : "초안 버리기"}
        </button>
      </div>
    </section>}
    {error && <p className="inline-error" role="alert">{error}</p>}
    {notice && <p className="delivery-notice" role="status">{notice}</p>}
  </>;
}
