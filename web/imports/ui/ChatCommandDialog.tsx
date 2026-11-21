import React, { useEffect, useRef, useState } from "react";
import { call, mutationUnconfirmed } from "../../client/data";
import { chatTitle, type ChatActivity, type ChatCommand, type ChatHistory, type ChatThread, type ContextUsage } from "../contracts/web";
import { ChatActivityLabel } from "./ChatActivity";
import { Modal } from "./Modal";

const titles = { status: "채팅 상태", rename: "채팅 이름 변경", review: "코드 리뷰", compact: "컨텍스트 압축", fork: "채팅 복제 (Fork)", exit: "웹 채팅 종료" };
const accepted = { rename: "채팅 이름을 변경했습니다.", review: "코드 리뷰를 시작했습니다.", compact: "컨텍스트 압축을 요청했습니다.", fork: "채팅을 복제했습니다.", exit: "웹 실행을 종료했습니다. 데스크톱 앱에서 이 채팅을 이어갈 수 있습니다." };

export function ChatCommandDialog({ command, chat, account, history, activity, known, usage, online, onClose, onDone }: {
  command: ChatCommand | "fork";
  chat: ChatThread;
  account: string;
  history?: ChatHistory;
  activity?: ChatActivity;
  known: boolean;
  usage?: ContextUsage;
  online: boolean;
  onClose: () => void;
  onDone: (notice: string, name?: string, fork?: ChatThread) => void;
}) {
  const [name, setName] = useState(() => chatTitle(chat));
  const [target, setTarget] = useState("changes");
  const [branch, setBranch] = useState("");
  const [busy, setBusy] = useState(false);
  const [unconfirmed, setUnconfirmed] = useState(false);
  const [error, setError] = useState("");
  const inFlight = useRef(false);
  const cancel = useRef<HTMLButtonElement>(null);
  const executable = command === "review" || command === "compact";
  const exiting = command === "exit";
  const active = activity?.state === "running" || activity?.state === "responding" ||
    activity?.state === "waiting" || history?.thread.status.type === "active";
  const waiting = executable && active;
  const externallyOwned = (executable || exiting) && !!history?.thread.externalOwner || (exiting && activity?.source === "external");
  const lastTurn = history?.turns.at(-1);
  const exitTurn = exiting && active ? activity?.turnId || (lastTurn?.status === "inProgress" ? lastTurn.id : undefined) : undefined;
  const exitUnavailable = exiting && (!history?.webOnly || active && !exitTurn);
  useEffect(() => { if (exiting) cancel.current?.focus(); }, [exiting]);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (command === "status" || !online || inFlight.current || waiting || externallyOwned || exitUnavailable || unconfirmed) return;
    inFlight.current = true;
    setBusy(true);
    setError("");
    try {
      const params = { thread_id: chat.id, account, ...(command === "rename" ? { name: name.trim() } : {}),
        ...(command === "review" && target === "base" ? { branch: branch.trim() } : {}), ...(exitTurn ? { turn_id: exitTurn } : {}) };
      const result = await call<{ name?: string; status?: string; thread?: ChatThread; thread_id?: string }>(`web.${command}`, params, crypto.randomUUID());
      const confirmed = command === "rename" ? !!result.name : exiting ? result.status === "released" && result.thread_id === chat.id :
        result.status === "accepted" && (command !== "fork" || !!result.thread?.id && result.thread.id !== chat.id);
      if (!confirmed) {
        setUnconfirmed(true);
        setError("명령 결과를 확인하지 못했습니다. 채팅 목록에서 실행 여부를 확인해 주세요.");
        return;
      }
      onDone(accepted[command], result.name, result.thread);
    } catch (reason) {
      const message = (reason as Error).message;
      setError(message);
      setUnconfirmed(mutationUnconfirmed(message));
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }

  return <Modal title={titles[command]} onClose={() => { if (!busy) onClose(); }}>
    {command === "status" ? <dl className="chat-command-status">
      <dt>채팅 ID</dt><dd><code>{chat.id}</code></dd>
      <dt>작업 폴더</dt><dd>{chat.cwd}</dd>
      <dt>실행 상태</dt><dd><ChatActivityLabel activity={activity ?? { state: known ? "idle" : "unknown", source: "web" }}
        known={known} lastTurn={history?.turns.at(-1)} /></dd>
      {history?.executionAccount && <><dt>실행 계정</dt><dd>{history.executionAccount}</dd></>}
      {usage && <><dt>최근 턴 컨텍스트</dt><dd>{usage.tokens.toLocaleString()}{usage.window ? ` / ${usage.window.toLocaleString()}` : ""} 토큰</dd></>}
    </dl> : <form className="dialog-form" onSubmit={event => void submit(event)}>
      {command === "rename" && <label>채팅 이름<input value={name} maxLength={200} required autoFocus
        disabled={busy} onChange={event => setName(event.target.value)} /></label>}
      {command === "review" && <>
        <label>리뷰 대상<select value={target} disabled={busy} onChange={event => setTarget(event.target.value)}>
          <option value="changes">커밋하지 않은 변경사항</option>
          <option value="base">기준 브랜치와 비교</option>
        </select></label>
        {target === "base" && <label>기준 브랜치<input value={branch} maxLength={200} required disabled={busy}
          onChange={event => setBranch(event.target.value)} placeholder="main" /></label>}
      </>}
      {command === "compact" && <p>Codex가 다음 작업에 사용할 대화 컨텍스트를 요약합니다.</p>}
      {command === "fork" && <p>‘{chatTitle(chat)}’의 저장된 대화를 복제해 새 채팅으로 엽니다.</p>}
      {exiting && !externallyOwned && <>
        <p>‘{chatTitle(chat)}’의 웹 실행을 종료하면 데스크톱 앱에서 이어갈 수 있습니다.</p>
        {active && <p>현재 작업을 중지합니다.</p>}
        {!!history?.queue?.length && <p>대기 중인 입력은 일시중지합니다.</p>}
        {exitUnavailable && <p>{history && !history.webOnly ? "웹 전용 실행에서 채팅을 종료할 수 있습니다." : "채팅의 실행 상태를 확인하는 중입니다."}</p>}
      </>}
      {externallyOwned ? <p>이 채팅을 열어 둔 Codex 앱이나 CLI에서 실행해 주세요.</p> : waiting && <p>현재 작업이 끝나면 실행할 수 있습니다.</p>}
      {error && <p role="alert" className="inline-error">{error}</p>}
      <div className="button-row">
        <button type="button" ref={cancel} disabled={busy} autoFocus={executable || command === "fork"} onClick={onClose}>취소</button>
        <button type="submit" className="primary" disabled={!online || busy || waiting || externallyOwned || exitUnavailable || unconfirmed ||
          command === "rename" && !name.trim() || command === "review" && target === "base" && !branch.trim()}>
          {busy ? "처리 중…" : command === "rename" ? "저장" : command === "review" ? "리뷰 시작" : command === "fork" ? "복제" : exiting ? "채팅 종료" : "압축 시작"}
        </button>
      </div>
    </form>}
  </Modal>;
}
