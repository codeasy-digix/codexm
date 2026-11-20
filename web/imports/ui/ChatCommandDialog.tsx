import React, { useRef, useState } from "react";
import { call, mutationUnconfirmed } from "../../client/data";
import { chatTitle, type ChatActivity, type ChatCommand, type ChatHistory, type ChatThread, type ContextUsage } from "../contracts/web";
import { ChatActivityLabel } from "./ChatActivity";
import { Modal } from "./Modal";

const titles = { status: "채팅 상태", rename: "채팅 이름 변경", review: "코드 리뷰", compact: "컨텍스트 압축" };
const accepted = { rename: "채팅 이름을 변경했습니다.", review: "코드 리뷰를 시작했습니다.", compact: "컨텍스트 압축을 요청했습니다." };

export function ChatCommandDialog({ command, chat, account, history, activity, known, usage, online, onClose, onDone }: {
  command: ChatCommand;
  chat: ChatThread;
  account: string;
  history?: ChatHistory;
  activity?: ChatActivity;
  known: boolean;
  usage?: ContextUsage;
  online: boolean;
  onClose: () => void;
  onDone: (notice: string, name?: string) => void;
}) {
  const [name, setName] = useState(() => chatTitle(chat));
  const [target, setTarget] = useState("changes");
  const [branch, setBranch] = useState("");
  const [busy, setBusy] = useState(false);
  const [unconfirmed, setUnconfirmed] = useState(false);
  const [error, setError] = useState("");
  const inFlight = useRef(false);
  const executable = command === "review" || command === "compact";
  const waiting = executable && (activity?.state === "running" || activity?.state === "responding" ||
    activity?.state === "waiting" || history?.thread.status.type === "active");
  const externallyOwned = executable && !!history?.thread.externalOwner;

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (command === "status" || !online || inFlight.current || waiting || externallyOwned || unconfirmed) return;
    inFlight.current = true;
    setBusy(true);
    setError("");
    try {
      const params = { thread_id: chat.id, account, ...(command === "rename" ? { name: name.trim() } : {}),
        ...(command === "review" && target === "base" ? { branch: branch.trim() } : {}) };
      const result = await call<{ name?: string; status?: string }>(`web.${command}`, params, crypto.randomUUID());
      if (command === "rename" ? !result.name : result.status !== "accepted") {
        throw new Error("명령 결과를 확인하지 못했습니다. 채팅에서 실행 여부를 확인해 주세요.");
      }
      onDone(accepted[command], result.name);
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
      {externallyOwned ? <p>이 채팅을 열어 둔 Codex 앱이나 CLI에서 실행해 주세요.</p> : waiting && <p>현재 작업이 끝나면 실행할 수 있습니다.</p>}
      {error && <p role="alert" className="inline-error">{error}</p>}
      <div className="button-row">
        <button type="button" disabled={busy} autoFocus={executable} onClick={onClose}>취소</button>
        <button type="submit" className="primary" disabled={!online || busy || waiting || externallyOwned || unconfirmed ||
          command === "rename" && !name.trim() || command === "review" && target === "base" && !branch.trim()}>
          {busy ? "처리 중…" : command === "rename" ? "저장" : command === "review" ? "리뷰 시작" : "압축 시작"}
        </button>
      </div>
    </form>}
  </Modal>;
}
