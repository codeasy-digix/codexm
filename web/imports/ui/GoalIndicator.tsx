import React from "react";
import type { ThreadGoal } from "../contracts/web";

const labels = {
  active: "목표 진행 중",
  paused: "목표 일시 정지",
  blocked: "목표 진행 대기",
  usageLimited: "사용 한도 도달",
  budgetLimited: "목표 예산 도달",
  complete: "목표 완료",
};

export function GoalIndicator({ goal }: { goal: ThreadGoal }) {
  return <details className="goal-indicator" data-status={goal.status}>
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
    </div>
  </details>;
}
