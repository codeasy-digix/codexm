import React, { useEffect, useState } from "react";
import { call } from "../../client/data";
import type { Data } from "../contracts/types";
export function Logs({
  jobs,
  selected = "",
}: {
  jobs: Data[];
  selected?: string;
}) {
  const [log, setLog] = useState<Data>({}),
    [scope, setScope] = useState(""),
    [account, setAccount] = useState(""),
    [period, setPeriod] = useState("0"),
    [reason, setReason] = useState(""),
    [error, setError] = useState("");
  const load = () =>
    void call("logs.read", { id: scope, limit: 500 })
      .then(setLog)
      .catch((e) => setError(e.reason || e.message));
  useEffect(load, [scope]);
  const from = period === "0" ? 0 : Date.now() - Number(period) * 86400000;
  return (
    <div>
      <h2>사용 기록</h2>
      <div className="toolbar">
        <select
          aria-label="로그 작업"
          value={scope}
          onChange={(e) => setScope(e.target.value)}
        >
          <option value="">모든 작업</option>
          {jobs.map((j) => (
            <option key={j.id} value={j.id}>
              {j.title || j.id}
            </option>
          ))}
        </select>
        <input
          aria-label="로그 계정"
          placeholder="계정 별칭"
          value={account}
          onChange={(e) => setAccount(e.target.value)}
        />
        <select
          aria-label="로그 기간"
          value={period}
          onChange={(e) => setPeriod(e.target.value)}
        >
          <option value="0">모든 기간</option>
          <option value="1">최근 24시간</option>
          <option value="7">최근 7일</option>
        </select>
        <input
          aria-label="변경 사유"
          placeholder="변경 사유 검색"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
        />
        <button onClick={load}>새로고침</button>
        {selected && (
          <button onClick={() => setScope(selected)}>선택 작업만</button>
        )}
      </div>
      <p className="muted">
        실행 시간에는 대기가 포함됩니다. 한도는 계정 전체 관측값이며 작업별
        정확한 소비율이 아닙니다.
      </p>
      {error && <p role="alert">{error}</p>}
      <table>
        <thead>
          <tr>
            <th>작업 / 계정</th>
            <th>시작</th>
            <th>실행 시간</th>
            <th>시작 / 최근 단기 잔여</th>
          </tr>
        </thead>
        <tbody>
          {(log.runs || [])
            .filter(
              (r: Data) =>
                (!account || r.Account === account) &&
                +new Date(r.EndedAt || Date.now()) >= from,
            )
            .map((r: Data) => {
              const remaining = (v?: Data) =>
                v?.primary?.used_percent == null
                  ? "미확인"
                  : `${100 - v.primary.used_percent}%`;
              return (
                <tr key={r.ID}>
                  <td>
                    {jobs.find((j) => j.id === r.JobID)?.title || r.JobID} /{" "}
                    {r.Account}
                  </td>
                  <td>{new Date(r.StartedAt).toLocaleString()}</td>
                  <td>
                    {Math.max(
                      0,
                      Math.floor(
                        (+new Date(r.EndedAt || Date.now()) -
                          +new Date(r.StartedAt)) /
                          1000,
                      ),
                    )}
                    초
                  </td>
                  <td>
                    {remaining(r.UsageStart)} / {remaining(r.UsageLatest)}
                  </td>
                </tr>
              );
            })}
        </tbody>
      </table>
      <h3>계정 변경</h3>
      {(log.changes || [])
        .filter(
          (c: Data) =>
            (!account || c.from === account || c.to === account) &&
            +new Date(c.at) >= from &&
            (!reason || c.reason?.includes(reason)),
        )
        .map((c: Data) => (
          <p key={c.id}>
            <strong>
              {c.from} → {c.to}
            </strong>{" "}
            · {c.reason}
            <br />
            <small>{new Date(c.at).toLocaleString()}</small>
          </p>
        ))}
    </div>
  );
}
