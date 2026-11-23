import React, { useEffect, useState } from "react";
import { call } from "../../client/data";
import type { AccountLimits as Limits } from "../contracts/web";

export function AccountLimits({ account }: { account: string }) {
  const [limits, setLimits] = useState<Limits>();
  useEffect(() => {
    let stopped = false;
    let pending = false;
    let timer: ReturnType<typeof setTimeout>;
    const refresh = async () => {
      if (stopped || pending || document.visibilityState === "hidden") return;
      clearTimeout(timer);
      pending = true;
      let delay = 60_000;
      try {
        const value = await call<Limits>("web.usage", { account });
        if (!stopped) setLimits(value);
        if (value.loading) delay = 1_500;
      } catch {
        if (!stopped) setLimits(undefined);
      } finally {
        pending = false;
        if (!stopped) timer = setTimeout(refresh, delay);
      }
    };
    void refresh();
    document.addEventListener("visibilitychange", refresh);
    return () => {
      stopped = true;
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, [account]);

  return <div className="account-limits" role="group" aria-label="선택 계정의 남은 한도">
    <LimitBar label="5h" description="5시간" remaining={limits?.fiveHour} loading={limits?.loading} />
    <LimitBar label="주간" description="주간" remaining={limits?.week} loading={limits?.loading} />
  </div>;
}

function LimitBar({ label, description, remaining, loading }: {
  label: string;
  description: string;
  remaining?: number | null;
  loading?: boolean;
}) {
  const name = `${description} 남은 한도`;
  const unknown = loading ? "확인 중" : "미확인";
  return <div className="account-limit" title={`${name} ${remaining == null ? unknown : `${remaining}%`}`}>
    <span>{label}</span>
    {remaining == null
      ? <span className="limit-track" aria-hidden="true" />
      : <meter aria-label={name} aria-valuetext={`${remaining}% 남음`} min={0} max={100} value={remaining} />}
    <span aria-label={remaining == null ? `${name} ${unknown}` : undefined}>{remaining == null ? "—" : `${remaining}%`}</span>
  </div>;
}
