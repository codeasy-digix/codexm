export type Data = Record<string, any>;
export type SessionEntry = {
  id: string;
  thread_id?: string;
  job_id?: string;
  title: string;
  cwd: string;
  account?: string;
  state: string;
  kind: string;
  managed: boolean;
  archived: boolean;
  model?: string;
  tokens_used?: number;
};
export const commands = [
  { name: "/manage", label: "작업 관리", view: "manage" },
  { name: "/accounts", label: "계정", view: "accounts" },
  { name: "/usage", label: "사용량", view: "usage" },
  { name: "/logs", label: "사용 기록", view: "logs" },
  { name: "/settings", label: "설정", view: "settings" },
  { name: "/status", label: "연결 상태", view: "status" },
  { name: "/manager", label: "관리자에게 요청 · 모델 호출", view: "manager" },
] as const;
export function directCommand(text: string) {
  return commands.find((c) => c.name === text.trim());
}
export function shouldSend(event: {
  key: string;
  shiftKey: boolean;
  isComposing: boolean;
  keyCode: number;
}) {
  return (
    event.key === "Enter" &&
    !event.shiftKey &&
    !event.isComposing &&
    event.keyCode !== 229
  );
}
export function sessionKey(
  host: string,
  store: string,
  entry?: SessionEntry | null,
) {
  return JSON.stringify([host, store, entry?.thread_id || entry?.id || "new"]);
}
export function remaining(window?: { usedPercent?: number } | null) {
  return typeof window?.usedPercent === "number"
    ? `${Math.max(0, 100 - window.usedPercent)}%`
    : "미확인";
}
