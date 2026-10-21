import React, { useEffect, useState } from "react";
import { call } from "../../client/data";
import { remaining, type Data } from "../contracts/types";
export type Run = (method: string, params?: Data) => Promise<any>;
const date = (v?: string | number) =>
  v
    ? new Date(typeof v === "number" ? v * 1000 : v).toLocaleString()
    : "미확인";
export function Accounts({
  snapshot,
  generation,
  run,
  login,
  setLogin,
}: {
  snapshot: Data;
  generation: string;
  run: Run;
  login: Data | null;
  setLogin: (value: Data | null) => void;
}) {
  const [account, setAccount] = useState(""),
    [mode, setMode] = useState("device"),
    [manager, setManager] = useState(snapshot.manager_account || ""),
    [error, setError] = useState("");
  const names: string[] = snapshot.account_names || [],
    observations: Data[] = snapshot.state?.accounts || [];
  useEffect(() => {
    let active = true;
    void call("login.status")
      .then((current) => {
        if (active) setLogin(current);
      })
      .catch(() => {
        if (active) setLogin(null);
      });
    return () => {
      active = false;
    };
  }, [generation]);
  useEffect(() => {
    if (!login || !["pending", "verifying"].includes(login.status)) return;
    const timer = setInterval(() => {
      void call("login.status", { login_id: login.login_id })
        .then(setLogin)
        .catch((e) => {
          setError(e.reason || e.message);
          setLogin(null);
        });
    }, 1000);
    return () => clearInterval(timer);
  }, [login?.login_id, login?.status]);
  const pending = login && ["pending", "verifying"].includes(login.status);
  const url = login?.auth_url || login?.verification_url;
  let href = "";
  try {
    const parsed = new URL(url);
    if (
      parsed.protocol === "https:" &&
      [
        "auth.openai.com",
        "auth0.openai.com",
        "chatgpt.com",
        "platform.openai.com",
      ].includes(parsed.hostname)
    )
      href = parsed.href;
  } catch {}
  return (
    <div>
      <h2>계정 관리</h2>
      <p>
        작업 우선순위: {(snapshot.priority || []).join(" → ")} · 관리자:{" "}
        {snapshot.manager_account || "미설정"} · 공용 기본 로그인:{" "}
        {snapshot.native_default_account || "미확인"}
      </p>
      <form
        className="form"
        onSubmit={(e) => {
          e.preventDefault();
          setError("");
          void run("login.start", { account, mode, generation })
            .then(setLogin)
            .catch((e) => setError(e.reason || e.message));
        }}
      >
        <fieldset>
          <legend>계정 추가 · 재로그인</legend>
          <div className="form-grid">
            <label>
              계정 별칭
              <input
                value={account}
                onChange={(e) => setAccount(e.target.value)}
                required
                pattern="[A-Za-z0-9_-]+"
                disabled={!!pending}
              />
            </label>
            <label>
              로그인 방식
              <select
                value={mode}
                onChange={(e) => setMode(e.target.value)}
                disabled={!!pending}
              >
                <option value="device">기기 코드</option>
                <option value="browser">브라우저</option>
              </select>
            </label>
          </div>
          <p>
            <button disabled={!!pending}>로그인 시작</button>
          </p>
        </fieldset>
      </form>
      {login && (
        <section className="card" aria-label="로그인 진행">
          <strong>
            {login.account} · {login.status}
          </strong>
          {pending && (
            <>
              <p>
                {href && (
                  <a href={href} target="_blank" rel="noreferrer">
                    공식 로그인 페이지 열기
                  </a>
                )}
              </p>
              {login.user_code && (
                <p>
                  기기 코드: <code>{login.user_code}</code>
                </p>
              )}
              <button
                onClick={() =>
                  void run("login.cancel", {
                    login_id: login.login_id,
                    generation,
                  })
                    .then(setLogin)
                    .catch(() => {})
                }
              >
                로그인 취소
              </button>
            </>
          )}
          <p>{login.error}</p>
        </section>
      )}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      {!snapshot.connected && (
        <form
          className="toolbar"
          onSubmit={(e) => {
            e.preventDefault();
            void run("service.start", { manager_account: manager }).catch(
              () => {},
            );
          }}
        >
          <label>
            관리자 계정
            <select
              value={manager}
              onChange={(e) => setManager(e.target.value)}
              required
            >
              <option value="">계정 선택</option>
              {names.map((n) => (
                <option key={n}>{n}</option>
              ))}
            </select>
          </label>
          <button>관리 서비스 시작</button>
        </form>
      )}
      <p>
        <button
          disabled={!snapshot.connected}
          onClick={() => void run("accounts.refresh").catch(() => {})}
        >
          모든 계정 한도 새로 조회
        </button>
      </p>
      <div className="cards">
        {names.map((name) => {
          const observation = observations.find((s) => s.account === name),
            bucket =
              observation?.rateLimitsByLimitId?.codex ||
              observation?.rateLimits;
          return (
            <section className="card" key={name}>
              <h3>{name}</h3>
              <p>
                {name === snapshot.manager_account ? "관리자 · " : ""}
                {name === snapshot.fallback_account ? "예비 계정 · " : ""}
                {name === snapshot.native_default_account
                  ? "공용 기본 로그인"
                  : ""}
              </p>
              <p>
                단기 잔여 {remaining(bucket?.primary)} · 장기 잔여{" "}
                {remaining(bucket?.secondary)}
              </p>
              <small>
                관측: {date(observation?.checked_at)}
                <br />
                단기 재설정: {date(bucket?.primary?.resetsAt)}
              </small>
              <p>{observation?.error}</p>
              <div className="toolbar">
                <button onClick={() => setAccount(name)}>재로그인</button>
                <button
                  onClick={() =>
                    void run("accounts.default", { account: name }).catch(
                      () => {},
                    )
                  }
                >
                  공용 기본 로그인으로 선택
                </button>
              </div>
            </section>
          );
        })}
      </div>
      {!names.length && (
        <p>저장된 계정이 없습니다. 계정 별칭을 입력하고 로그인하세요.</p>
      )}
      <p className="muted">
        한도 비율은 계정 전체 사용량입니다. 공용 기본 로그인 변경은 이후 공식
        CLI·GUI 실행에 적용됩니다. 현재 실행 중인 작업의 계정은 작업 상세에서
        바꿀 수 있습니다.
      </p>
    </div>
  );
}
