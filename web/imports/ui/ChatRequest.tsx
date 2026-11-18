import React, { useRef, useState } from "react";
import { call } from "../../client/data";
import type { PendingRequest } from "../contracts/web";

type Question = {
  id: string;
  header: string;
  question: string;
  options?: { label: string; description: string }[] | null;
  isOther?: boolean;
  isSecret?: boolean;
};
const decisionLabels: Record<string, string> = {
  accept: "이번 실행 승인",
  acceptForSession: "이 세션에서 승인",
  decline: "거절하고 계속",
  cancel: "거절하고 작업 중지",
};

export function ChatRequest({ request, thread, onDone }: {
  request: PendingRequest;
  thread: string;
  onDone: () => void;
}) {
  const [choices, setChoices] = useState<Record<string, number | "other">>({});
  const [text, setText] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [finished, setFinished] = useState(false);
  const [error, setError] = useState("");
  const inFlight = useRef(false);
  const asksQuestion = request.method === "item/tool/requestUserInput";
  const questions = Array.isArray(request.params.questions) ? request.params.questions as Question[] : [];
  const answers = questions.map(question => {
    const choice = choices[question.id];
    return typeof choice === "number" ? question.options?.[choice]?.label || "" :
      !question.options?.length || choice === "other" ? text[question.id] || "" : "";
  });
  const disabled = busy || finished;

  async function respond(response: Record<string, unknown>) {
    if (inFlight.current || finished) return;
    inFlight.current = true;
    setBusy(true);
    setError("");
    try {
      await call("web.respond", { thread_id: thread, account: request.account, request_id: request.request_id, response }, crypto.randomUUID());
      setFinished(true);
      setText({});
      onDone();
    } catch (error) {
      const message = (error as Error).message;
      setError(message);
      if (/outcome_unknown|확인하지 못했|대기 시간이 초과|no longer pending/i.test(message)) {
        setFinished(true);
        setText({});
        onDone();
      }
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }

  const { command, cwd, reason, grantRoot } = request.params;
  const decisions = (request.decisions || []).filter(decision => Object.hasOwn(decisionLabels, decision));
  return <section className="approval" aria-label={asksQuestion ? "추가 확인" : "실행 승인"}>
    <h3>{asksQuestion ? "추가 확인이 필요합니다" : request.method.includes("fileChange") ? "파일 변경 승인" : "명령 실행 승인"}</h3>
    {asksQuestion ? <form onSubmit={event => {
      event.preventDefault();
      if (!questions.length || answers.some(answer => !answer.trim())) return;
      void respond({ answers: Object.fromEntries(questions.map((question, index) => [
        question.id, { answers: [answers[index]] },
      ])) });
    }}>
      {questions.map(question => {
        const name = request.request_id + "-" + question.id;
        const options = question.options || [];
        return <fieldset key={question.id} disabled={disabled}>
          <legend>{question.header}</legend>
          <p>{question.question}</p>
          {options.map((option, index) => <label className="request-option" key={index}>
            <input type="radio" name={name} checked={choices[question.id] === index}
              onChange={() => setChoices(old => ({ ...old, [question.id]: index }))} />
            <span>{option.label}{option.description && <small>{option.description}</small>}</span>
          </label>)}
          {options.length > 0 && question.isOther && <label className="request-option">
            <input type="radio" name={name} checked={choices[question.id] === "other"}
              onChange={() => setChoices(old => ({ ...old, [question.id]: "other" }))} />
            <span>직접 입력</span>
          </label>}
          {(!options.length || choices[question.id] === "other") && <label>
            {question.header} 답변
            <input autoComplete="off" type={question.isSecret ? "password" : "text"} value={text[question.id] || ""}
              onChange={event => setText(old => ({ ...old, [question.id]: event.target.value }))} />
          </label>}
        </fieldset>;
      })}
      <button className="primary" disabled={disabled || !questions.length || answers.some(answer => !answer.trim())}>답변 전달</button>
    </form> : <>
      {typeof reason === "string" && <p>{reason}</p>}
      {typeof command === "string" && <pre>{command}</pre>}
      {typeof cwd === "string" && <p>작업 폴더 {cwd}</p>}
      {typeof grantRoot === "string" && <p>쓰기 요청 위치 {grantRoot}</p>}
      <div className="button-row">{decisions.map(decision => <button key={decision}
        className={decision === "accept" ? "primary" : ""}
        onClick={() => void respond({ decision })} disabled={disabled}>{decisionLabels[decision]}</button>)}</div>
      {!decisions.length && <p>이 승인 유형은 웹에서 처리할 수 없습니다.</p>}
    </>}
    {error ? <p role="alert">{error}</p> : finished ? <p role="status">응답을 전달했습니다.</p> : null}
  </section>;
}
