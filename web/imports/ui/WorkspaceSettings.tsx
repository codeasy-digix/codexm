import React, { useState } from "react";
import { logout } from "../../client/data";
import { Accounts } from "./Accounts";
import { Modal } from "./Modal";
import { NotificationSettings } from "./NotificationSettings";
import { ShortcutSettings } from "./ShortcutSettings";
import type { Theme } from "./theme";

const sections = { appearance: "화면", notifications: "알림", shortcuts: "단축키", accounts: "계정 관리" } as const;
type Section = keyof typeof sections;

export function WorkspaceSettings({ theme, onTheme, thread, accounts, shortcuts, onClose }: {
  theme: Theme;
  onTheme: (theme: Theme) => void;
  thread?: string;
  accounts: React.ComponentProps<typeof Accounts>["accounts"];
  shortcuts: Omit<React.ComponentProps<typeof ShortcutSettings>, "onClose">;
  onClose: () => void;
}) {
  const [section, setSection] = useState<Section>("appearance");
  const [error, setError] = useState("");
  const [signedOut, setSignedOut] = useState(false);
  return <Modal title="설정" onClose={onClose} className="workspace-settings">
    <nav className="settings-nav" aria-label="설정 메뉴">
      {(Object.keys(sections) as Section[]).map(key =>
        <button type="button" key={key} aria-pressed={section === key} onClick={() => setSection(key)}>{sections[key]}</button>)}
    </nav>
    <div className="settings-panel">
      {section === "appearance" && <label>테마<select value={theme} onChange={event => onTheme(event.target.value as Theme)}>
        <option value="system">시스템 설정</option><option value="light">라이트 모드</option><option value="dark">다크 모드</option>
      </select></label>}
      {section === "notifications" && <NotificationSettings thread={thread} />}
      {section === "shortcuts" && <ShortcutSettings {...shortcuts} onSave={bindings => {
        shortcuts.onSave(bindings);
        onClose();
      }} onClose={() => setSection("appearance")} />}
      {section === "accounts" && <>
        <Accounts accounts={accounts} />
        <div className="settings-session">
          <button type="button" disabled={signedOut} onClick={async () => {
            setSignedOut(true);
            setError("");
            try { await logout(); onClose(); }
            catch (reason) { setError((reason as Error).message); setSignedOut(false); }
          }}>로그아웃</button>
          {error && <p className="inline-error" role="alert">{error}</p>}
        </div>
      </>}
    </div>
  </Modal>;
}
