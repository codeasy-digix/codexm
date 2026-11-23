import React from "react";
import { workingActivity, type ChatActivity, type ChatTurn } from "../contracts/web";

export function ChatActivityLabel({ activity, known, lastTurn, footer = false }: {
  activity?: ChatActivity;
  known: boolean;
  lastTurn?: ChatTurn;
  footer?: boolean;
}) {
  const external = activity?.source === "external";
  const state = activity?.state;
  const running = workingActivity(activity);
  const observed = known || state === "idle";
  let text = state === "responding" ? "응답 생성 중" :
    state === "running" ? "작업 중" :
    state === "waiting" ? "확인 대기" :
    state === "unknown" || !observed ? "상태 확인 중" :
    lastTurn?.status === "completed" ? "작업 완료" :
    lastTurn?.status === "failed" ? "작업 실패" :
    lastTurn?.status === "interrupted" && !external ? "중단됨" : "대기 중";
  if (external) {
    text = !footer && state === "idle" ? "다른 창에서 열림" : "다른 창 · " + text;
  }
  if (!footer && !activity) return null;
  return <span
    className={`activity-label${footer ? " activity-footer" : ""}${running ? " is-working" : ""}`}
    role={footer ? "status" : undefined}
    aria-label={footer ? "현재 작업 상태" : undefined}
    data-state={state || (known ? "idle" : "unknown")}
  >{(running || state === "waiting") && <span className="activity-dot" aria-hidden="true" />}{text}</span>;
}
