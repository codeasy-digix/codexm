import React, { useEffect, useRef, useState } from "react";
import { call, exchange, useAccess, useStateData } from "../../client/data";
import { shouldSend, remaining, type SessionEntry } from "../contracts/types";
import "./styles/native.css";

export function App() {
  const access = useAccess(), data = useStateData(), snapshot = data.snapshot || {};
  const [token, setToken] = useState(""), [view, setView] = useState("chats");
  const [query, setQuery] = useState(""), [archived, setArchived] = useState(false);
  const [selected, setSelected] = useState<SessionEntry | null>(null);
  const [account, setAccount] = useState(""), [session, setSession] = useState("");
  const [terminal, setTerminal] = useState<any>(null), [message, setMessage] = useState("");
  const [error, setError] = useState(""), [busy, setBusy] = useState(false);
  const [usage, setUsage] = useState<Record<string, any>>({});
  const epoch = useRef(0), log = useRef<HTMLPreElement>(null), stick = useRef(true);
  const drafts = useRef<Record<string, string>>({});
  const accounts: any[] = snapshot.accounts || [];
  const chats: SessionEntry[] = (snapshot.sessions || []).filter((x: SessionEntry) =>
    (archived || !x.archived) && `${x.title} ${x.id} ${x.cwd} ${x.account || ""}`.toLowerCase().includes(query.toLowerCase()));
  function choose(chat: SessionEntry) {
    if (selected) drafts.current[selected.id] = message;
    epoch.current++; setSelected(chat); setAccount(""); setSession(""); setTerminal(null);
    setMessage(drafts.current[chat.id] || ""); setError(""); stick.current = true;
  }
  useEffect(() => {
    if (!session || !access.ready) return;
    let stopped = false, timer: ReturnType<typeof setTimeout>;
    const refresh = async () => {
      try { const next = await call("terminal.read", { session }); if (!stopped) setTerminal(next); }
      catch (e) { if (!stopped) setError((e as Error).message); }
      finally { if (!stopped) timer = setTimeout(refresh, 600); }
    };
    void refresh(); return () => { stopped = true; clearTimeout(timer); };
  }, [session, access.ready]);
  useEffect(() => { if (stick.current && log.current) log.current.scrollTop = log.current.scrollHeight; }, [terminal?.text]);
  async function open() {
    if (!selected || !account || busy) return;
    const generation = epoch.current; setBusy(true); setError("");
    try {
      const opened = await call("terminal.open", { thread_id: selected.id, account }, crypto.randomUUID());
      if (generation === epoch.current) { setSession(opened.session); stick.current = true; }
    } catch (e) { if (generation === epoch.current) setError((e as Error).message); }
    finally { setBusy(false); }
  }
  async function input(key?: string) {
    if (!session || busy || (!key && !message.trim())) return;
    const generation = epoch.current, sent = message;
    setBusy(true); setError("");
    try {
      await call("terminal.input", { session, ...(key ? { key } : { text: sent }) }, crypto.randomUUID());
      if (!key && generation === epoch.current) { setMessage(""); if (selected) drafts.current[selected.id] = ""; }
      stick.current = true;
    } catch (e) { if (generation === epoch.current) setError(`${(e as Error).message} · 전송 결과는 터미널에서 확인하세요. 입력은 자동 재전송하지 않습니다.`); }
    finally { setBusy(false); }
  }
  async function close() {
    if (!session || !confirm("이 터미널의 Codex CLI를 종료할까요? 진행 중인 작업은 중단됩니다. 대화 기록은 보존됩니다.")) return;
    const generation = epoch.current;
    setBusy(true);
    try { await call("terminal.close", { session }, crypto.randomUUID()); if (generation === epoch.current) { setSession(""); setTerminal(null); } }
    catch (e) { if (generation === epoch.current) setError((e as Error).message); } finally { setBusy(false); }
  }
  if (!access.ready) return <main className="login"><h1>codexm</h1><p>이 Mac의 채팅과 Codex CLI</p>
    <form onSubmit={e => { e.preventDefault(); void exchange(token).then(() => setToken("")).catch(e => setError(e.message)); }}>
      <label>로컬 접속 코드<input type="password" value={token} onChange={e => setToken(e.target.value)} /></label>
      <button>연결</button></form><p role="alert">{error || access.message}</p></main>;
  return <div className="app"><header><strong>codexm</strong><nav>
    <button aria-pressed={view === "chats"} onClick={() => setView("chats")}>채팅</button>
    <button aria-pressed={view === "accounts"} onClick={() => setView("accounts")}>계정</button>
  </nav><span>로컬 · 공식 Codex CLI</span></header>
  {(error || data.connectionError) && <p role="alert" className="error">{error || data.connectionError}</p>}
  {view === "accounts" ? <main className="account-page"><h1>계정</h1><p>로그인과 기본 계정 변경은 <code>codexm account</code>를 사용합니다. 자동 계정 교체는 사용하지 않습니다.</p>
    {accounts.map(a => <section key={a.name}><strong>{a.name}</strong> {a.default && <span>기본 계정</span>} {a.current && <span>현재 환경</span>}
      <button disabled={busy} onClick={async () => { setBusy(true); try { const result = await call("accounts.refresh", { account: a.name }); setUsage(v => ({ ...v, [a.name]: result })); } catch (e) { setError((e as Error).message); } finally { setBusy(false); } }}>사용량 확인</button>
      {usage[a.name] && <p>{usage[a.name].plan || ""} · 5h {remaining(usage[a.name].rateLimits?.primary)} · week {remaining(usage[a.name].rateLimits?.secondary)} {usage[a.name].error}</p>}
    </section>)}<pre>codexm account NAME{`\n`}codexm account NAME --login --browser{`\n`}codexm account NAME --set-default</pre></main> : <main className="workspace">
    <aside><label>채팅 검색<input value={query} onChange={e => setQuery(e.target.value)} placeholder="제목, 프로젝트, 계정, ID" /></label>
      <label className="checkbox"><input type="checkbox" checked={archived} onChange={e => setArchived(e.target.checked)} />보관된 채팅 포함</label>
      <p>{chats.length}개 · 모든 프로젝트</p><div className="chat-list">{chats.map(chat => <button key={chat.id} className={selected?.id === chat.id ? "selected" : ""} onClick={() => choose(chat)}>
        <strong>{chat.title}</strong><small>{chat.account || "이전 계정 기록 없음"}{chat.archived && " · 보관됨"}</small><small>{chat.cwd}</small>
      </button>)}</div></aside>
    <section className="chat"><h1>{selected?.title || "채팅을 선택하세요"}</h1>
      {selected ? <><p className="metadata">{selected.id} · 이전 계정: {selected.account || "기록 없음"}<br />{selected.cwd}</p>
        <div className="launch"><label>이번에 사용할 계정<select value={account} onChange={e => { epoch.current++; setAccount(e.target.value); setSession(""); setTerminal(null); }}>
          <option value="">계정 선택</option><option value="default">기본 계정</option>{accounts.map(a => <option key={a.name} value={a.name}>{a.name}</option>)}
        </select></label><button disabled={!account || busy} onClick={() => void open()}>Codex CLI로 이어가기</button></div>
        {!session && <p>계정을 선택하면 공식 CLI에서 이 채팅의 기록을 열고 이어갑니다.</p>}
      </> : <p>채팅을 고른 다음 사용할 계정을 선택하세요.</p>}
      {session && <><div className="terminal-status"><code>{terminal?.attach || `tmux attach -t ${session}`}</code><span>{terminal?.alive ? terminal?.launch?.ready ? "CLI 실행 중" : "CLI 준비 중" : "CLI 종료됨"}</span><button disabled={busy} onClick={() => void close()}>터미널 종료</button></div>
        <pre role="log" aria-label="공식 Codex CLI 화면" ref={log} className="terminal" onScroll={() => { const el = log.current!; stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 50; }}>{terminal?.text || terminal?.launch?.error || "공식 CLI를 여는 중…"}</pre>
        <div className="keys">{[["Up", "↑"], ["Down", "↓"], ["Enter", "Enter"], ["Escape", "Esc"], ["C-c", "Ctrl+C"], ["Tab", "Tab"]].map(([key, label]) => <button key={key} disabled={busy || !terminal?.alive || !terminal?.launch?.ready} onClick={() => void input(key)}>{label}</button>)}</div>
        <label>CLI에 전달할 입력<textarea value={message} onChange={e => setMessage(e.target.value)} onKeyDown={e => { if (shouldSend({ key: e.key, shiftKey: e.shiftKey, isComposing: e.nativeEvent.isComposing, keyCode: e.keyCode })) { e.preventDefault(); void input(); } }} placeholder="메시지 또는 /명령 · Shift+Enter 줄바꿈" /></label>
        <button disabled={busy || !terminal?.alive || !terminal?.launch?.ready || !message.trim()} onClick={() => void input()}>CLI에 전달</button><small>웹을 닫아도 CLI는 위 tmux 세션에서 계속 실행됩니다.</small>
      </>}
    </section></main>}</div>;
}
