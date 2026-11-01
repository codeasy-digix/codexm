import React, { useEffect, useRef, useState } from "react";
import { call } from "../../client/data";
import { remaining } from "../contracts/types";

export function Accounts({ accounts }: { accounts: any[] }) {
  const [name, setName] = useState(""), [mode, setMode] = useState("device");
  const [login, setLogin] = useState<any>(null), [busy, setBusy] = useState(false);
  const [error, setError] = useState(""), [notice, setNotice] = useState("");
  const [usage, setUsage] = useState<Record<string, any>>({});
  const lastStatus = useRef("");
  const pending = login?.status === "pending" || login?.status === "verifying";
  useEffect(() => {
    let stopped = false, timer: ReturnType<typeof setTimeout>;
    const refresh = async () => {
      try {
        const next = await call("login.status");
        if (!stopped) {
          setLogin(next);
          const statusKey = `${next.login_id}:${next.status}`;
          if (statusKey !== lastStatus.current) {
            if (next.status === "succeeded") setNotice(`${next.account} 계정 로그인이 완료됐습니다.`);
            if (next.status === "failed" || next.status === "expired") setError(next.error || "로그인이 만료되었습니다. 다시 시작하세요.");
            lastStatus.current = statusKey;
          }
        }
      } catch (e) { if (!stopped) setError((e as Error).message); }
      finally { if (!stopped) timer = setTimeout(refresh, 1500); }
    };
    void refresh(); return () => { stopped = true; clearTimeout(timer); };
  }, []);
  async function start(account = name) {
    if (busy || pending || !account.trim()) return;
    setBusy(true); setError(""); setNotice("");
    try { setLogin(await call("login.start", { account: account.trim(), mode }, crypto.randomUUID())); }
    catch (e) { setError((e as Error).message); }
    finally { setBusy(false); }
  }
  async function manage(method: string, account: string) {
    const prompt = method === "accounts.remove"
      ? `${account} 계정의 등록을 해제할까요? 인증 파일·대화·실행 중 작업·기본 인증은 보존합니다.`
      : `${account} 계정을 새 터미널과 다음 GUI 실행의 기본 계정으로 지정할까요? 실행 중인 작업은 바뀌지 않습니다.`;
    if (busy || !confirm(prompt)) return;
    setBusy(true); setError(""); setNotice("");
    try {
      await call(method, { account }, crypto.randomUUID());
      setNotice(method === "accounts.remove" ? `${account} 등록을 해제했습니다. 인증 파일과 대화는 보존했습니다.` : `${account} 계정을 기본 계정으로 지정했습니다. 실행 중인 GUI에는 재실행 후 적용됩니다.`);
    } catch (e) { setError((e as Error).message); }
    finally { setBusy(false); }
  }
  return <main className="account-page"><h1>계정 관리</h1>
    <p>계정 추가·재로그인·기본 계정 지정·등록 해제를 관리합니다.</p>
    {error && <p role="alert" className="error">{error}</p>}{notice && <p role="status">{notice}</p>}
    <section className="account-add"><h2>계정 추가</h2>
      <form onSubmit={e => { e.preventDefault(); void start(); }}>
        <label>계정 닉네임<input value={name} onChange={e => setName(e.target.value)} maxLength={64} pattern="[A-Za-z0-9][A-Za-z0-9_-]*" required placeholder="예: work" disabled={busy || pending} /></label>
        <label>인증 방식<select value={mode} onChange={e => setMode(e.target.value)} disabled={busy || pending}>
          <option value="device">기기 코드 · 다른 기기에서도 가능</option><option value="browser">브라우저 · 서버와 같은 장비</option>
        </select></label><button disabled={busy || pending || !name.trim()}>추가 / 로그인</button>
      </form>
      {mode === "browser" && <p>로그인 링크는 Codex가 실행되는 장비의 브라우저에서 열어야 합니다. 다른 기기에서 접속 중이면 기기 코드를 선택하세요.</p>}
    </section>
    {pending && <section className="login-progress"><h2>{login.account} 로그인</h2>
      {login.status === "verifying" ? <p>로그인 결과를 확인하고 있습니다…</p> : <>
        {(login.verification_url || login.auth_url) && <p><a href={login.verification_url || login.auth_url} target="_blank" rel="noopener noreferrer">공식 로그인 페이지 열기</a></p>}
        {login.user_code && <p>기기 코드: <code>{login.user_code}</code></p>}
      </>}
      <button disabled={busy} onClick={async () => { setBusy(true); try { setLogin(await call("login.cancel", { login_id: login.login_id }, crypto.randomUUID())); setNotice("로그인을 취소했습니다. 기존 인증은 보존했습니다."); } catch (e) { setError((e as Error).message); } finally { setBusy(false); } }}>로그인 취소</button>
    </section>}
    {accounts.map(a => <section className="account-row" key={a.name} aria-label={`${a.name} 계정`}><div><strong>{a.name}</strong> {a.default && <span>기본 계정</span>} {a.current && <span>현재 환경</span>}</div>
      <div className="account-actions">
        <button disabled={busy} onClick={async () => { setBusy(true); setError(""); try { const result = await call("accounts.refresh", { account: a.name }); setUsage(v => ({ ...v, [a.name]: result })); } catch (e) { setError((e as Error).message); } finally { setBusy(false); } }}>사용량 확인</button>
        <button disabled={busy || pending} onClick={() => { setName(a.name); void start(a.name); }}>재로그인</button>
        <button disabled={busy || pending || a.default} onClick={() => void manage("accounts.setDefault", a.name)}>기본 계정으로 지정</button>
        <button disabled={busy || pending} onClick={() => void manage("accounts.remove", a.name)}>등록 해제</button>
      </div>
      {usage[a.name] && <p>{usage[a.name].email || ""} {usage[a.name].plan || ""} · 5h {remaining(usage[a.name].rateLimits?.primary)} · week {remaining(usage[a.name].rateLimits?.secondary)} {usage[a.name].error}</p>}
    </section>)}
    <p>등록 해제는 파일 삭제가 아닙니다. 같은 닉네임으로 로그인하면 다시 등록할 수 있습니다.</p>
  </main>;
}
