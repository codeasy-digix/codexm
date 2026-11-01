import React, { useEffect, useRef, useState } from "react";
import { call, exchange, login, logout, useAccess, useStateData } from "../../client/data";
import { shouldSend, type SessionEntry } from "../contracts/types";
import { preferredAccount, terminalFailure } from "../contracts/selection";
import { Accounts } from "./Accounts";
import "./styles/native.css";

export function App() {
  const access = useAccess(), data = useStateData(), snapshot = data.snapshot || {};
  const [token, setToken] = useState(""), [view, setView] = useState("chats");
  const [username, setUsername] = useState(""), [password, setPassword] = useState("");
  const [query, setQuery] = useState(""), [archived, setArchived] = useState(false);
  const [details, setDetails] = useState(false);
  const [selected, setSelected] = useState<SessionEntry | null>(null);
  const [account, setAccount] = useState(""), [session, setSession] = useState("");
  const [terminal, setTerminal] = useState<any>(null), [message, setMessage] = useState("");
  const [error, setError] = useState(""), [busy, setBusy] = useState(false);
  const epoch = useRef(0), log = useRef<HTMLPreElement>(null), stick = useRef(true);
  const list = useRef<HTMLDivElement>(null), listOffset = useRef(0);
  const drafts = useRef<Record<string, string>>({});
  const accounts: any[] = snapshot.accounts || [];
  const failure = terminalFailure(terminal);
  const chats: SessionEntry[] = (snapshot.sessions || []).filter((x: SessionEntry) =>
    (archived || !x.archived) && `${x.title} ${x.id} ${x.cwd} ${x.account || ""}`.toLowerCase().includes(query.toLowerCase()));
  function choose(chat: SessionEntry) {
    if (selected) drafts.current[selected.id] = message;
    listOffset.current = list.current?.scrollTop || 0;
    epoch.current++; setSelected(chat); setAccount(preferredAccount(chat.account, accounts)); setSession(""); setTerminal(null);
    setMessage(drafts.current[chat.id] || ""); setError(""); setDetails(true); stick.current = true;
  }
  useEffect(() => { if (!details && list.current) list.current.scrollTop = listOffset.current; }, [details]);
  useEffect(() => {
    if (snapshot.accounts && account && account !== "default" && !accounts.some(a => a.name === account)) {
      epoch.current++; setAccount("default"); setSession(""); setTerminal(null);
    }
  }, [snapshot.accounts, account]);
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
    if (!session || busy || !terminal?.alive || !terminal?.launch?.ready || (!key && !message.trim())) return;
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
  if (!access.ready) return <main className="login"><h1>codexm</h1><p>로그인 후 채팅과 Codex CLI를 사용할 수 있습니다.</p>
    <form onSubmit={e => { e.preventDefault(); if (busy || !access.modeReady) return; setBusy(true); setError("");
      void (access.password ? login(username, password) : exchange(token)).then(() => { setToken(""); setPassword(""); }).catch(e => setError(e.message)).finally(() => setBusy(false)); }}>
      {access.password ? <><label>아이디<input name="username" autoComplete="username" required maxLength={64} value={username} onChange={e => setUsername(e.target.value)} /></label>
        <label>비밀번호<input name="password" type="password" autoComplete="current-password" required maxLength={1024} value={password} onChange={e => setPassword(e.target.value)} /></label></>
        : <label>로컬 접속 코드<input type="password" value={token} onChange={e => setToken(e.target.value)} /></label>}
      <button disabled={busy || !access.modeReady}>{busy ? "로그인 중…" : access.password ? "로그인" : "연결"}</button></form><p role="alert">{error || access.message}</p></main>;
  return <div className="app"><header><strong>codexm</strong><nav>
    <button aria-pressed={view === "chats"} onClick={() => { setView("chats"); setDetails(false); }}>채팅</button>
    <button aria-pressed={view === "accounts"} onClick={() => setView("accounts")}>계정</button>
  </nav><span>공식 Codex CLI</span><button onClick={() => { void logout().then(() => { epoch.current++; setSelected(null); setTerminal(null); setSession(""); setMessage(""); setDetails(false); drafts.current = {}; setError(""); }).catch(e => setError(e.message)); }}>로그아웃</button></header>
  {(error || data.connectionError) && <p role="alert" className="error">{error || data.connectionError}</p>}
  {view === "accounts" ? <Accounts accounts={accounts} /> : <main className={`workspace${details ? " show-details" : ""}`}>
    <aside><label>채팅 검색<input value={query} onChange={e => setQuery(e.target.value)} placeholder="제목, 프로젝트, 계정, ID" /></label>
      <label className="checkbox"><input type="checkbox" checked={archived} onChange={e => setArchived(e.target.checked)} />보관된 채팅 포함</label>
      <p>{chats.length}개 · 모든 프로젝트</p><div className="chat-list" ref={list} aria-label="채팅 목록">{chats.map(chat => <button key={chat.id} className={selected?.id === chat.id ? "selected" : ""} onClick={() => choose(chat)}>
        <strong title={chat.title}>{chat.title}</strong><small>{chat.account || "이전 계정 기록 없음"}{chat.archived && " · 보관됨"}</small><small>{chat.cwd}</small>
      </button>)}</div></aside>
    <section className="chat"><button className="mobile-back" onClick={() => setDetails(false)}>← 채팅 목록</button><h1>{selected?.title || "채팅을 선택하세요"}</h1>
      {selected ? <><p className="metadata">{selected.id} · 이전 계정: {selected.account || "기록 없음"}<br />{selected.cwd}</p>
        <div className="launch"><label>이번에 사용할 계정<select value={account} onChange={e => { epoch.current++; setAccount(e.target.value); setSession(""); setTerminal(null); }}>
          <option value="default">기본 계정</option>{accounts.map(a => <option key={a.name} value={a.name}>{a.name}</option>)}
        </select></label><button disabled={!account || busy} onClick={() => void open()}>Codex CLI로 이어가기</button></div>
        {!session && <p>계정을 선택하면 보관·복원 후 같은 채팅 ID로 공식 CLI를 엽니다. 다른 GUI·CLI가 계속 소유 중이면 인계 오류를 표시합니다.</p>}
      </> : <p>채팅을 고른 다음 사용할 계정을 선택하세요.</p>}
      {session && <><div className="terminal-status"><code>{terminal?.attach || `tmux attach -t ${session}`}</code><span>{failure ? "CLI 인계 또는 실행 실패" : terminal?.alive ? terminal?.launch?.ready ? "CLI 실행 중" : "CLI 준비 중" : "CLI 종료됨"}</span><button disabled={busy} onClick={() => void close()}>터미널 종료</button></div>
        {failure && <div role="alert" className="terminal-error"><strong>{failure.title}</strong><p>{failure.help}</p>{failure.detail && <details><summary>오류 상세</summary><pre>{failure.detail}</pre></details>}<button disabled={busy} onClick={() => void open()}>다시 시도</button></div>}
        <pre role="log" aria-label="공식 Codex CLI 화면" ref={log} className="terminal" onScroll={() => { const el = log.current!; stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 50; }}>{terminal?.text || terminal?.launch?.error || "공식 CLI를 여는 중…"}</pre>
        <div className="keys">{[["Up", "↑"], ["Down", "↓"], ["Enter", "Enter"], ["Escape", "Esc"], ["C-c", "Ctrl+C"], ["Tab", "Tab"]].map(([key, label]) => <button key={key} disabled={busy || !terminal?.alive || !terminal?.launch?.ready} onClick={() => void input(key)}>{label}</button>)}</div>
        <label>CLI에 전달할 입력<textarea value={message} onChange={e => setMessage(e.target.value)} onKeyDown={e => { if (shouldSend({ key: e.key, shiftKey: e.shiftKey, isComposing: e.nativeEvent.isComposing, keyCode: e.keyCode })) { e.preventDefault(); void input(); } }} placeholder="메시지 또는 /명령 · Shift+Enter 줄바꿈" /></label>
        <button disabled={busy || !terminal?.alive || !terminal?.launch?.ready || !message.trim()} onClick={() => void input()}>CLI에 전달</button><small>웹을 닫아도 CLI는 위 tmux 세션에서 계속 실행됩니다.</small>
      </>}
    </section></main>}</div>;
}
