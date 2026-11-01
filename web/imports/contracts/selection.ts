export function preferredAccount(previous: string | undefined, accounts: { name: string }[]) {
  return previous && accounts.some(account => account.name === previous) ? previous : "default";
}

export function terminalFailure(terminal: any): { title: string; help: string; detail: string } | null {
  const detail = terminal?.launch?.error || "";
  if (detail.startsWith("official archive failed;")) return {
    title: "채팅을 넘겨받지 못했습니다 · 보관 단계 실패",
    help: "이 채팅을 사용 중인 GUI 또는 CLI가 있으면 원래 화면에서 해당 채팅을 보관하거나 세션을 종료한 후 다시 시도하세요. 계정만 바꿔도 사용 중인 세션의 잠금이 풀리지는 않습니다.", detail,
  };
  if (detail.startsWith("official unarchive failed;")) return {
    title: "채팅을 넘겨받지 못했습니다 · 보관 해제 실패",
    help: "보관된 채팅 목록과 원래 세션 상태를 확인한 뒤 다시 시도하세요. 새 CLI는 시작하지 않았습니다.", detail,
  };
  if (detail) return { title: "Codex CLI 준비에 실패했습니다", help: "아래 오류에서 계정 로그인과 프로젝트 경로를 확인한 뒤 다시 시도하세요.", detail };
  if (terminal && !terminal.alive && terminal.exit_code != null && terminal.exit_code !== 0) return {
    title: `Codex CLI가 오류로 종료됐습니다 · 종료 코드 ${terminal.exit_code}`,
    help: "아래 터미널 출력에서 원인을 확인하세요. 입력은 자동으로 다시 보내지 않습니다.", detail: "",
  };
  return null;
}
