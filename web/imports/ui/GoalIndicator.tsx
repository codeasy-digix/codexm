import React, { useState } from "react";
import { call } from "../../client/data";
import type { ThreadGoal } from "../contracts/web";
import { Modal } from "./Modal";

const labels = {
  active: "목표 진행 중",
  paused: "목표 일시 정지",
  blocked: "목표 진행 대기",
  usageLimited: "사용 한도 도달",
  budgetLimited: "목표 예산 도달",
  complete: "목표 완료",
};

export function GoalIndicator({ goal, thread, account, canControl, online, switchAccount, onChanged }: {
  goal: ThreadGoal; thread: string; account: string; canControl: boolean;
  online: boolean; switchAccount: boolean; onChanged: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [deleteGoal, setDeleteGoal] = useState<ThreadGoal>();
  const canResume = ["paused", "blocked", "usageLimited"].includes(goal.status) || goal.status === "active" && switchAccount;

  async function change(mode: "pause" | "resume" | "clear", target = goal) {
    if (busy || !online || !target.createdAt) return;
    setBusy(true);
    setError("");
    try {
      await call("web.goalChange", { thread_id: thread, account, mode, switch_account: switchAccount,
        expected_goal: { objective: target.objective, createdAt: target.createdAt } }, crypto.randomUUID());
      setDeleteGoal(undefined);
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setBusy(false);
      onChanged();
    }
  }

  return <><details className="goal-indicator" data-status={goal.status}>
    <summary>
      <span className="goal-dot" aria-hidden="true" />
      <strong>{labels[goal.status] || "채팅 목표"}</strong>
      <span className="goal-preview">{goal.objective}</span>
      <span className="goal-chevron" aria-hidden="true">⌄</span>
    </summary>
    <div className="goal-details">
      <p>{goal.objective}</p>
      {(goal.tokensUsed > 0 || goal.tokenBudget != null) && <small>
        {goal.tokensUsed.toLocaleString()} 토큰 사용{goal.tokenBudget != null && ` / ${goal.tokenBudget.toLocaleString()}`}
      </small>}
      {canControl && goal.createdAt && <div className="goal-actions">
        {goal.status === "active" && <button type="button" onClick={() => void change("pause")} disabled={!online || busy}>일시 정지</button>}
        {canResume && <button type="button" onClick={() => void change("resume")} disabled={!online || busy}>
          {switchAccount ? "선택 계정으로 재개" : "목표 재개"}
        </button>}
        <button type="button" className="danger" onClick={() => { setError(""); setDeleteGoal(goal); }} disabled={!online || busy}>목표 삭제</button>
      </div>}
      {!deleteGoal && error && <p role="alert" className="inline-error">{error}</p>}
    </div>
  </details>{deleteGoal && <Modal title="목표 삭제" onClose={() => { if (!busy) setDeleteGoal(undefined); }}>
    <p className="dialog-chat-title">{deleteGoal.objective}</p>
    <p>이 채팅에서 목표를 삭제할까요? 대화는 그대로 남습니다.</p>
    {error && <p role="alert" className="inline-error">{error}</p>}
    <div className="button-row">
      <button type="button" onClick={() => setDeleteGoal(undefined)} disabled={busy}>취소</button>
      <button type="button" className="danger-button" onClick={() => void change("clear", deleteGoal)} disabled={busy || !online}>목표 삭제</button>
    </div>
  </Modal>}</>;
}
