import React, { useEffect, useRef, useState } from "react";
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
    [error, setError] = useState(""),
    [removal, setRemoval] = useState<{ account: string; revision: string } | null>(null),
    [removing, setRemoving] = useState(false);
  const removeDialog = useRef<HTMLDialogElement>(null);
  const names: string[] = snapshot.account_names || [],
    observations: Data[] = snapshot.state?.accounts || [];
  useEffect(() => {
    const dialog = removeDialog.current;
    if (removal && dialog && !dialog.open) dialog.showModal();
    if (!removal && dialog?.open) dialog.close();
  }, [removal]);
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
      <dialog
        ref={removeDialog}
        aria-labelledby="remove-account-title"
        onCancel={(e) => {
          if (removing) e.preventDefault();
          else setRemoval(null);
        }}
      >
        <h3 id="remove-account-title">계정 등록 삭제</h3>
        <p><strong>{removal?.account}</strong> 계정을 목록과 작업 우선순위에서 삭제합니다.</p>
        <p>이 계정의 작업 배정을 해제하고 소유 worker를 안전하게 인계합니다. 중지된 작업은 유지하며, 다음 실행 때마다 가용 계정을 우선순위로 선택합니다. 가용 계정이 없으면 작업을 보존하고 대기합니다. 관리자 계정은 역할을 먼저 변경하세요.</p>
        <p>로그인 파일·공용 기본 로그인·대화 기록은 보존합니다. 같은 별칭으로 다시 등록할 수 있습니다.</p>
        {error && <p role="alert" className="error">{error}</p>}
        <div className="toolbar">
          <button disabled={removing} onClick={() => setRemoval(null)}>취소</button>
          <button
            disabled={removing || !removal}
            onClick={async () => {
              if (!removal || removing) return;
              setRemoving(true);
              setError("");
              try {
                await run("accounts.remove", removal);
                setRemoval(null);
              } catch (e: any) {
                setError(e.reason || e.message);
              } finally {
                setRemoving(false);
              }
            }}
          >{removing ? "삭제 중…" : "등록 삭제"}</button>
        </div>
      </dialog>
      <p>
        작업 우선순위: {(snapshot.priority || []).join(" → ")} · 관리자:{" "}
        {snapshot.manager_account || "미설정"} · native Codex 기본 계정:{" "}
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
                  ? "native Codex 기본 계정"
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
                  새 터미널의 native 기본 계정으로 저장
                </button>
                <button
                  aria-label={`${name} 등록 삭제`}
                  disabled={!snapshot.account_removal_available || !!(pending && login?.account === name)}
                  onClick={() => {
                    setError("");
                    setRemoval({ account: name, revision: snapshot.config_revision });
                  }}
                >등록 삭제</button>
              </div>
            </section>
          );
        })}
      </div>
      {!names.length && (
        <p>저장된 계정이 없습니다. 계정 별칭을 입력하고 로그인하세요.</p>
      )}
      <p className="muted">
        한도 비율은 계정 전체 사용량입니다. native 기본 계정 변경은 이후 공식
        CLI·GUI 실행에 적용됩니다. 터미널에서 명시적으로 선택한 계정은 유지합니다.
        터미널 선택은 codexm account NAME, 새 터미널 기본값은 --set-default입니다.
        작업 우선순위·관리자·예비 계정은 설정에서 관리합니다. 현재 작업의 계정은 상세에서
        바꿀 수 있습니다.
      </p>
    </div>
  );
}
