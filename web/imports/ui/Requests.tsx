import React from "react";
import type { Data } from "../contracts/types";
import type { Run } from "./Accounts";
export function Requests({ job, run }: { job?: Data; run: Run }) {
  if (!job) return null;
  return (
    <>
      {(job.pending_requests || []).map((request: Data) => {
        const p = request.params || {},
          reply = (result: Data) =>
            void run("jobs.reply", {
              id: job.id,
              run_id: job.run_id,
              request_id: request.id,
              result,
            }).catch(() => {});
        return (
          <fieldset
            disabled={!!job.target_account}
            className="request"
            key={`${job.run_id}:${JSON.stringify(request.id)}`}
          >
            <legend>응답 대기</legend>
            {request.method === "item/tool/requestUserInput" ? (
              <form
                className="form"
                onSubmit={(e) => {
                  e.preventDefault();
                  const data = new FormData(e.currentTarget),
                    answers: Data = {};
                  for (const q of p.questions)
                    answers[q.id] = { answers: [String(data.get(q.id))] };
                  reply({ answers });
                }}
              >
                {(p.questions || []).map((q: Data) => (
                  <label key={q.id}>
                    {q.question || q.header}
                    <input name={q.id} required list={`answers-${q.id}`} />
                    <datalist id={`answers-${q.id}`}>
                      {q.options?.map((o: Data) => (
                        <option key={o.label} value={o.label}>
                          {o.description}
                        </option>
                      ))}
                    </datalist>
                  </label>
                ))}
                <button>답변 보내기</button>
              </form>
            ) : [
                "item/commandExecution/requestApproval",
                "item/fileChange/requestApproval",
              ].includes(request.method) ? (
              <>
                <pre>{p.command || p.reason || "변경 승인이 필요합니다."}</pre>
                <div className="toolbar">
                  {[
                    ["accept", "이번 실행 허용"],
                    ["decline", "거절"],
                    ["cancel", "취소"],
                  ].map(([decision, label]) => (
                    <button key={decision} onClick={() => reply({ decision })}>
                      {label}
                    </button>
                  ))}
                </div>
              </>
            ) : request.method === "item/permissions/requestApproval" ? (
              <>
                <p>{p.reason}</p>
                <pre>{JSON.stringify(p.permissions, null, 2)}</pre>
                <button
                  onClick={() =>
                    reply({ permissions: p.permissions || {}, scope: "turn" })
                  }
                >
                  이번 턴에 허용
                </button>
                <button
                  onClick={() => reply({ permissions: {}, scope: "turn" })}
                >
                  거절
                </button>
              </>
            ) : request.method === "mcpServer/elicitation/request" ? (
              <>
                <p>{p.message}</p>
                {p.mode === "form" &&
                Object.values(p.requestedSchema?.properties || {}).every(
                  (v: any) =>
                    ["string", "number", "integer", "boolean"].includes(v.type),
                ) ? (
                  <form
                    className="form"
                    onSubmit={(e) => {
                      e.preventDefault();
                      const data = new FormData(e.currentTarget),
                        content: Data = {};
                      for (const [key, def] of Object.entries(
                        p.requestedSchema.properties,
                      ) as [string, Data][]) {
                        const v = data.get(key);
                        if (v !== null && v !== "")
                          content[key] =
                            def.type === "boolean"
                              ? v === "on"
                              : ["number", "integer"].includes(def.type)
                                ? Number(v)
                                : v;
                      }
                      reply({ action: "accept", content });
                    }}
                  >
                    {(
                      Object.entries(p.requestedSchema.properties) as [
                        string,
                        Data,
                      ][]
                    ).map(([key, def]) => (
                      <label key={key}>
                        {def.title || key}
                        {def.enum ? (
                          <select name={key}>
                            {def.enum.map((v: string) => (
                              <option key={v}>{v}</option>
                            ))}
                          </select>
                        ) : (
                          <input
                            name={key}
                            required={p.requestedSchema.required?.includes(key)}
                            type={
                              def.type === "boolean"
                                ? "checkbox"
                                : ["number", "integer"].includes(def.type)
                                  ? "number"
                                  : "text"
                            }
                            min={def.minimum}
                            max={def.maximum}
                            step={def.type === "integer" ? 1 : "any"}
                            minLength={def.minLength}
                            maxLength={def.maxLength}
                          />
                        )}
                      </label>
                    ))}
                    <button>입력 전달</button>
                  </form>
                ) : (
                  <p>이 요청은 공식 Codex 터미널에서 확인하세요.</p>
                )}
                <button onClick={() => reply({ action: "decline" })}>
                  거절
                </button>
                <button onClick={() => reply({ action: "cancel" })}>
                  취소
                </button>
              </>
            ) : (
              <p>이 요청은 공식 Codex 터미널에서 확인하세요.</p>
            )}
          </fieldset>
        );
      })}
    </>
  );
}
