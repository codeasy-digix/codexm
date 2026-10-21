import React, { useEffect, useState } from "react";
import { call } from "../../client/data";
import type { Data } from "../contracts/types";
import type { Run } from "./Accounts";
export function Settings({ run }: { run: Run }) {
  const [doc, setDoc] = useState<Data>(),
    [values, setValues] = useState<Data>({}),
    [section, setSection] = useState("accounts"),
    [error, setError] = useState("");
  async function load() {
    try {
      const result = await call("settings.read");
      setDoc(result);
      setValues(result.values);
      setError("");
    } catch (e: any) {
      setError(e.reason || e.message);
    }
  }
  useEffect(() => {
    void load();
  }, []);
  function update(key: string, value: unknown) {
    setValues((v) => ({ ...v, [key]: value }));
  }
  if (!doc) return <p>{error || "설정 불러오는 중…"}</p>;
  const names: string[] = doc.accounts || [];
  return (
    <div>
      <h2>설정</h2>
      <div className="settings">
        <nav aria-label="설정 항목">
          {[
            ["accounts", "계정 정책"],
            ["runtime", "감시와 실행"],
            ["notifications", "알림"],
          ].map(([key, title]) => (
            <button
              aria-current={section === key}
              key={key}
              onClick={() => setSection(key)}
            >
              {title}
            </button>
          ))}
        </nav>
        <form
          className="form"
          onSubmit={(e) => {
            e.preventDefault();
            setError("");
            void run("settings.save", { revision: doc.revision, values })
              .then((next) => {
                setDoc(next);
                setValues(next.values);
              })
              .catch((e) => setError(e.reason || e.message));
          }}
        >
          {section === "accounts" && (
            <>
              <label>
                관리자 계정
                <select
                  value={values.manager_account}
                  required
                  onChange={(e) => update("manager_account", e.target.value)}
                >
                  <option value="">선택</option>
                  {names.map((n) => (
                    <option key={n}>{n}</option>
                  ))}
                </select>
              </label>
              <label>
                작업 계정 우선순위 · 쉼표로 구분
                <input
                  value={(values.priority || []).join(", ")}
                  onChange={(e) =>
                    update(
                      "priority",
                      e.target.value
                        .split(",")
                        .map((s) => s.trim())
                        .filter(Boolean),
                    )
                  }
                />
              </label>
              <label>
                예비 계정
                <select
                  value={values.fallback_account}
                  onChange={(e) => update("fallback_account", e.target.value)}
                >
                  <option value="">사용하지 않음</option>
                  {names.map((n) => (
                    <option key={n}>{n}</option>
                  ))}
                </select>
              </label>
              <p>
                예비 계정은 우선순위 마지막에 둡니다. 상위 계정의 소진이 모두
                확인되었을 때 사용하며 미확인은 소진으로 처리하지 않습니다.
              </p>
            </>
          )}
          {section === "runtime" &&
            [
              [
                "preferred_recheck_seconds",
                "예비 계정 사용 중 우선 계정 확인 (초)",
                60,
                86400,
              ],
              ["reconcile_interval_seconds", "작업 상태 확인 (초)", 1, 300],
              ["max_parallel_jobs", "동시 실행 작업 수", 1, 32],
              ["usage_sample_seconds", "사용량 관측 간격 (초)", 10, 3600],
            ].map(([key, title, min, max]) => (
              <label key={key}>
                {title}
                <input
                  type="number"
                  required
                  min={min}
                  max={max}
                  value={values[key]}
                  onChange={(e) => update(String(key), Number(e.target.value))}
                />
              </label>
            ))}
          {section === "notifications" && (
            <label>
              <span>
                <input
                  type="checkbox"
                  checked={!!values.five_hour_discord}
                  onChange={(e) =>
                    update("five_hour_discord", e.target.checked)
                  }
                />{" "}
                5시간 한도 소진 시 기존 Discord 알림 사용
              </span>
              <small>
                저장된 알림 설정으로 전체 계정과 현재 작업 상태를 전달합니다.
              </small>
            </label>
          )}
          <p className="muted">{doc.application}</p>
          <div className="toolbar">
            <button>설정 저장</button>
            <button type="button" onClick={() => void load()}>
              저장된 설정 다시 불러오기
            </button>
          </div>
          {error && (
            <p role="alert" className="error">
              {error}
            </p>
          )}
        </form>
      </div>
    </div>
  );
}
