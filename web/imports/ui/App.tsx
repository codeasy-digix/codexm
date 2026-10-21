import React, { useCallback, useEffect, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import { call, exchange, useAccess, useStateData } from "../../client/data";
import {
  commands,
  directCommand,
  remaining,
  sessionKey,
  shouldSend,
  type Data,
  type SessionEntry,
} from "../contracts/types";
import { Accounts } from "./Accounts";
import { Settings } from "./Settings";
import { Logs } from "./Logs";
import { Requests } from "./Requests";
import "./styles/base.css";
import "./styles/layout.css";
function readSaved<T>(key: string, fallback: T): T {
  try {
    return JSON.parse(localStorage.getItem(key) || "null") ?? fallback;
  } catch {
    return fallback;
  }
}
function save(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {}
}
const mergeTurns = (a: Data[], b: Data[]) => [
  ...new Map([...a, ...b].map((t) => [t.id, t])).values(),
];

export function App() {
  const access = useAccess(),
    data = useStateData(),
    caps = data.capabilities || {},
    snap = data.snapshot || {},
    jobs: Data[] = snap.state?.jobs || [];
  const [token, setToken] = useState(""),
    [notice, setNotice] = useState(""),
    [view, setView] = useState("chat"),
    [sidebar, setSidebar] = useState(false),
    [detailsOpen, setDetailsOpen] = useState(false);
  const [sessions, setSessions] = useState<SessionEntry[]>([]),
    [cursor, setCursor] = useState(""),
    [total, setTotal] = useState(0),
    [search, setSearch] = useState(""),
    [project, setProject] = useState(""),
    [filterAccount, setFilterAccount] = useState(""),
    [filterState, setFilterState] = useState(""),
    [managed, setManaged] = useState(false),
    [activeOnly, setActiveOnly] = useState(false),
    [starOnly, setStarOnly] = useState(false);
  const [selected, setSelected] = useState<SessionEntry | null>(null),
    [stars, setStars] = useState<string[]>([]),
    [detail, setDetail] = useState<Data>({}),
    [draft, setDraft] = useState(""),
    [history, setHistory] = useState<string[]>([]),
    [busy, setBusy] = useState(0),
    [steer, setSteer] = useState(false);
  const [cwd, setCwd] = useState(""),
    [kind, setKind] = useState("chat"),
    [model, setModel] = useState(""),
    [effort, setEffort] = useState(""),
    [account, setAccount] = useState(""),
    [title, setTitle] = useState(""),
    [goal, setGoal] = useState(""),
    [budget, setBudget] = useState(""),
    [readOnly, setReadOnly] = useState(false),
    [check, setCheck] = useState(""),
    [parent, setParent] = useState("");
  const [targetAccount, setTargetAccount] = useState(""),
    [login, setLogin] = useState<Data | null>(null),
    [managerText, setManagerText] = useState(""),
    [managerAll, setManagerAll] = useState(false),
    [uncertain, setUncertain] = useState<Data[]>([]),
    [operationResult, setOperationResult] = useState<Data>();
  const input = useRef<HTMLTextAreaElement>(null),
    transcript = useRef<HTMLDivElement>(null),
    sticky = useRef(true),
    locked = useRef(new Map<string, Promise<any>>()),
    detailEpoch = useRef(0),
    listEpoch = useRef(0),
    listSize = useRef(0);
  listSize.current = sessions.length;
  const scope = caps.host ? `codexm:${caps.host}:${caps.store}` : "",
    key = sessionKey(caps.host || "", caps.store || "", selected);
  const job =
    detail.job?.id === selected?.job_id
      ? detail.job
      : jobs.find((j) => j.id === selected?.job_id);
  const canWrite = snap.connected && caps.features?.journal;
  useEffect(() => {
    const current = jobs.find((j) => j.id === selected?.job_id);
    if (!current?.thread_id || selected?.thread_id === current.thread_id)
      return;
    const entry = { ...selected!, thread_id: current.thread_id };
    const nextKey = sessionKey(caps.host, caps.store, entry);
    for (const field of ["draft", "history"]) {
      const value = readSaved(scope + ":" + field + ":" + key, null);
      if (value !== null) save(scope + ":" + field + ":" + nextKey, value);
    }
    if (stars.includes(key)) {
      const next = stars.map((star) => (star === key ? nextKey : star));
      setStars(next);
      save(scope + ":stars", next);
    }
    setSelected(entry);
    save(scope + ":selected", entry);
  }, [jobs, selected?.job_id, selected?.thread_id, scope, key]);
  useEffect(() => {
    if (!scope) return;
    setSelected(readSaved(scope + ":selected", null));
    setStars(readSaved(scope + ":stars", []));
    setUncertain(readSaved(scope + ":operations", []));
  }, [scope]);
  useEffect(() => {
    setDraft(readSaved(scope + ":draft:" + key, ""));
    setHistory(readSaved(scope + ":history:" + key, []));
    setDetail({});
    detailEpoch.current++;
    sticky.current = true;
    setSteer(false);
    setTimeout(() => input.current?.focus(), 0);
  }, [key, scope]);
  const updateDraft = (text: string) => {
    setDraft(text);
    if (scope) save(scope + ":draft:" + key, text);
  };
  const choose = (entry: SessionEntry | null) => {
    setSelected(entry);
    save(scope + ":selected", entry);
    setView("chat");
    setSidebar(false);
    if (entry?.cwd) setCwd(entry.cwd);
  };
  function recordOperations(next: Data[]) {
    setUncertain(next);
    save(scope + ":operations", next);
  }
  const run = async (method: string, params: Data = {}) => {
    const signature = JSON.stringify([method, params]);
    const existing = locked.current.get(signature);
    if (existing) return existing;
    const spec = (caps.methods || []).find((m: Data) => m.name === method),
      mutation = spec?.mutation;
    const id = crypto.randomUUID();
    if (
      mutation &&
      ![
        "login.start",
        "login.cancel",
        "accounts.default",
        "terminal.open",
        "service.start",
      ].includes(method)
    ) {
      const pending = readSaved<Data[]>(scope + ":operations", []);
      recordOperations([
        ...pending,
        { id, method, job: params.id, at: new Date().toISOString() },
      ]);
    }
    setBusy((v) => v + 1);
    setNotice("");
    const promise = call(method, params, id)
      .then((result) => {
        recordOperations(
          readSaved<Data[]>(scope + ":operations", []).filter(
            (p) => p.id !== id,
          ),
        );
        return result;
      })
      .catch((error) => {
        if (error.error !== "outcome_unknown")
          recordOperations(
            readSaved<Data[]>(scope + ":operations", []).filter(
              (p) => p.id !== id,
            ),
          );
        setNotice(
          `${error.reason || error.message}${error.error === "outcome_unknown" ? ` · 요청 ${id}` : ""}`,
        );
        throw error;
      })
      .finally(() => {
        locked.current.delete(signature);
        setBusy((v) => v - 1);
      });
    locked.current.set(signature, promise);
    return promise;
  };
  const loadList = useCallback(
    async (next = "", refresh = false) => {
      if (!access.ready || !scope) return;
      const epoch = ++listEpoch.current;
      try {
        const page = await call("sessions.list", {
          search,
          cursor: next,
          project,
          account: filterAccount,
          state: filterState,
          managed,
          active: activeOnly,
        });
        if (epoch !== listEpoch.current) return;
        setSessions((old) =>
          next
            ? [
                ...new Map(
                  [...old, ...page.sessions].map((e) => [e.id, e]),
                ).values(),
              ]
            : refresh && old.length > 50
              ? [
                  ...new Map(
                    [...page.sessions, ...old.slice(50)].map((e) => [e.id, e]),
                  ).values(),
                ]
              : page.sessions,
        );
        if (!refresh || listSize.current <= 50) setCursor(page.cursor);
        setTotal(page.total);
        if (page.warning) setNotice(page.warning);
      } catch (e: any) {
        setNotice(e.reason || e.message);
      }
    },
    [
      access.ready,
      scope,
      search,
      project,
      filterAccount,
      filterState,
      managed,
      activeOnly,
    ],
  );
  useEffect(() => {
    const timer = setTimeout(() => void loadList(), 180);
    return () => {
      clearTimeout(timer);
      listEpoch.current++;
    };
  }, [loadList]);
  useEffect(() => {
    if (!access.ready) return;
    const timer = setInterval(() => {
      void loadList("", true);
    }, 4000);
    return () => clearInterval(timer);
  }, [loadList, cursor, access.ready]);
  useEffect(() => {
    if (!access.ready || !snap.state?.instance) return;
    const timer = setTimeout(() => void loadList("", true), 50);
    return () => clearTimeout(timer);
  }, [snap.state?.instance, snap.state?.sequence, access.ready, loadList]);
  useEffect(() => {
    if (!access.ready || !selected || !snap.connected) return;
    let stopped = false,
      running = false;
    const epoch = detailEpoch.current;
    const refresh = async () => {
      if (running || stopped) return;
      running = true;
      try {
        const result = await call(
          "jobs.details",
          selected.job_id
            ? { id: selected.job_id }
            : { thread_id: selected.thread_id || selected.id },
        );
        if (!stopped && epoch === detailEpoch.current)
          setDetail((old) => ({
            ...result,
            thread: result.thread
              ? {
                  ...result.thread,
                  turns: mergeTurns(
                    old.thread?.turns || [],
                    result.thread.turns || [],
                  ),
                }
              : undefined,
            cursor: old.olderLoaded ? old.cursor : result.cursor,
            olderLoaded: old.olderLoaded,
          }));
      } catch (e: any) {
        if (!stopped) setNotice(e.reason || e.message);
      } finally {
        running = false;
      }
    };
    void refresh();
    const timer = setInterval(() => void refresh(), 750);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  }, [access.ready, key, selected?.job_id, snap.connected]);
  useEffect(() => {
    if (sticky.current && transcript.current)
      transcript.current.scrollTop = transcript.current.scrollHeight;
  }, [detail]);
  useEffect(() => {
    if (!access.ready || !snap.connected) return;
    const params = { id: selected?.job_id || "", client_id: "web" };
    void call("viewer.heartbeat", params).catch(() => {});
    const timer = setInterval(
      () => void call("viewer.heartbeat", params).catch(() => {}),
      15000,
    );
    return () => {
      clearInterval(timer);
      void call("viewer.detach", { client_id: "web" }).catch(() => {});
    };
  }, [selected?.job_id, access.ready, snap.connected]);
  async function older() {
    const element = transcript.current,
      height = element?.scrollHeight || 0,
      top = element?.scrollTop || 0,
      epoch = detailEpoch.current;
    try {
      const result = await call("conversation.read", {
        ...(selected?.job_id
          ? { id: selected.job_id }
          : { thread_id: selected?.thread_id || selected?.id }),
        cursor: detail.cursor,
      });
      if (epoch !== detailEpoch.current) return;
      sticky.current = false;
      setDetail((old) => ({
        ...old,
        cursor: result.cursor,
        olderLoaded: true,
        thread: {
          ...old.thread,
          turns: mergeTurns(
            result.thread?.turns || [],
            old.thread?.turns || [],
          ),
        },
      }));
      requestAnimationFrame(() => {
        if (element) element.scrollTop = top + element.scrollHeight - height;
      });
    } catch (e: any) {
      setNotice(e.reason || e.message);
    }
  }
  async function submit() {
    const text = draft.trim();
    const creatingGroup = !selected && kind === "group";
    if ((!text && !creatingGroup) || busy) return;
    const direct = directCommand(text);
    if (direct) {
      setView(direct.view);
      updateDraft("");
      return;
    }
    if (text.startsWith("/")) {
      setNotice("지원되는 직접 명령을 선택하거나 공식 Codex 터미널을 여세요.");
      return;
    }
    if (
      uncertain.some(
        (p) => p.method === "jobs.send" || p.method === "jobs.create",
      )
    ) {
      setNotice(
        "결과가 확인되지 않은 요청이 있습니다. 연결 상태에서 요청과 대화를 먼저 확인하세요.",
      );
      setView("status");
      return;
    }
    try {
      let result;
      if (selected) {
        if (!selected.job_id) {
          setNotice("대화를 관리 등록한 뒤 실행권을 가져오세요.");
          setDetailsOpen(true);
          return;
        }
        result = await run("jobs.send", { id: selected.job_id, text, steer });
      } else {
        if (!cwd.startsWith("/")) {
          setNotice("새 작업의 절대 프로젝트 경로를 입력하세요.");
          return;
        }
        const completion = check.trim() ? JSON.parse(check) : [];
        result = await run("jobs.create", {
          cwd,
          kind,
          model,
          effort,
          account,
          title,
          prompt: creatingGroup ? "" : text,
          goal: kind === "goal" ? goal : "",
          token_budget: kind === "goal" && budget ? Number(budget) : undefined,
          read_only: readOnly,
          completion_command: completion,
          parent_id: parent,
        });
      }
      const previous = [text, ...history.filter((v) => v !== text)].slice(
        0,
        50,
      );
      save(scope + ":history:" + key, previous);
      setHistory(previous);
      updateDraft("");
      sticky.current = true;
      if (!selected && result?.id) {
        save(
          scope + ":history:" + sessionKey(caps.host, caps.store, result),
          previous,
        );
        choose({
          id: result.id,
          job_id: result.id,
          thread_id: result.thread_id,
          title: result.title,
          cwd: result.cwd,
          account: result.account,
          state: result.state,
          kind: result.kind,
          managed: true,
          archived: false,
        });
        void loadList();
      }
    } catch (e: any) {
      setNotice(e.reason || e.message);
    }
  }
  const toggleStar = () => {
    const next = stars.includes(key)
      ? stars.filter((s) => s !== key)
      : [...stars, key];
    setStars(next);
    save(scope + ":stars", next);
  };
  const action = (method: string, params: Data = {}) =>
    void run(method, { id: job?.id, ...params })
      .then(() => {
        if (method === "jobs.unregister") {
          choose(
            selected?.thread_id
              ? {
                  ...selected,
                  id: selected.thread_id,
                  job_id: undefined,
                  managed: false,
                }
              : null,
          );
          setDetail({});
        }
        void loadList();
      })
      .catch(() => {});
  if (!access.ready)
    return (
      <main className="access">
        <h1>codexm</h1>
        <p>이 머신의 대화와 계정을 관리하려면 로컬 접속 코드를 입력하세요.</p>
        <p>
          <code>codexm web</code>으로 브라우저를 열거나 실행기가 안내한 코드
          파일을 사용하세요.
        </p>
        <form
          className="form"
          onSubmit={(e) => {
            e.preventDefault();
            void exchange(token)
              .then(() => setToken(""))
              .catch((e) => setNotice(e.message));
          }}
        >
          <label>
            로컬 접속 코드
            <input
              type="password"
              autoComplete="off"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              required
              autoFocus
            />
          </label>
          <button>연결</button>
        </form>
        <p role="status">{notice || access.message}</p>
      </main>
    );
  return (
    <main className={`app ${sidebar ? "sidebar-open" : ""}`}>
      <aside className="sidebar">
        <header>
          <strong>codexm</strong>
          <button className="mobile-toggle" onClick={() => setSidebar(false)}>
            닫기
          </button>
        </header>
        <button onClick={() => choose(null)}>새 채팅</button>
        <input
          type="search"
          aria-label="대화 검색"
          placeholder="대화 검색"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <details>
          <summary>필터</summary>
          <label>
            프로젝트 경로
            <input
              value={project}
              onChange={(e) => setProject(e.target.value)}
            />
          </label>
          <label>
            계정
            <select
              value={filterAccount}
              onChange={(e) => setFilterAccount(e.target.value)}
            >
              <option value="">전체</option>
              {(snap.account_names || []).map((n: string) => (
                <option key={n}>{n}</option>
              ))}
            </select>
          </label>
          <label>
            상태
            <input
              value={filterState}
              onChange={(e) => setFilterState(e.target.value)}
              placeholder="running, paused…"
            />
          </label>
          <label>
            <span>
              <input
                type="checkbox"
                checked={managed}
                onChange={(e) => setManaged(e.target.checked)}
              />
              관리 작업만
            </span>
          </label>
          <label>
            <span>
              <input
                type="checkbox"
                checked={activeOnly}
                onChange={(e) => setActiveOnly(e.target.checked)}
              />
              보관 대화 제외
            </span>
          </label>
          <label>
            <span>
              <input
                type="checkbox"
                checked={starOnly}
                onChange={(e) => setStarOnly(e.target.checked)}
              />
              즐겨찾기만
            </span>
          </label>
        </details>
        <small>{total}개 대화</small>
        <div className="sessions">
          {sessions
            .filter(
              (e) =>
                !starOnly ||
                stars.includes(sessionKey(caps.host, caps.store, e)),
            )
            .map((e) => (
              <button
                className="session"
                key={e.id}
                aria-current={selected?.id === e.id}
                onClick={() => choose(e)}
              >
                <span>
                  {stars.includes(sessionKey(caps.host, caps.store, e))
                    ? "★ "
                    : ""}
                  {e.title || "새 대화"}
                </span>
                <small>
                  {e.managed ? "관리 · " : ""}
                  {e.account ? e.account + " · " : ""}
                  {e.state}
                </small>
              </button>
            ))}
          {cursor && (
            <button onClick={() => void loadList(cursor)}>대화 더 보기</button>
          )}
        </div>
        <nav aria-label="관리 메뉴">
          {commands
            .filter((c) => c.view !== "manager" && c.view !== "manage")
            .map((c) => (
              <button
                key={c.name}
                onClick={() => setView(c.view)}
                aria-current={view === c.view}
              >
                {c.label}
              </button>
            ))}
        </nav>
        <small>
          {snap.connected ? "관리 서비스 연결됨" : "관리 서비스 중지됨"}
        </small>
      </aside>
      <section className="main">
        <header className="topbar">
          <button className="mobile-toggle" onClick={() => setSidebar(true)}>
            메뉴
          </button>
          <h1>
            {view === "chat"
              ? selected?.title || "새 채팅"
              : commands.find((c) => c.view === view)?.label || "작업 관리"}
          </h1>
          {view !== "chat" && (
            <button onClick={() => setView("chat")}>대화로</button>
          )}
          {selected && (
            <>
              <button onClick={toggleStar} aria-label="즐겨찾기">
                {stars.includes(key) ? "★" : "☆"}
              </button>
              <button
                onClick={() => {
                  setDetailsOpen((v) => !v);
                  setView("chat");
                }}
              >
                작업 상세
              </button>
            </>
          )}
        </header>
        <div className="notice" role="status">
          {notice ||
            data.connectionError ||
            snap.message ||
            (busy
              ? "처리 중…"
              : !data.snapshot
                ? "관리 상태 불러오는 중…"
                : !snap.connected
                  ? "계정 화면에서 로그인하고 관리 서비스를 시작하세요."
                  : "")}
        </div>
        {view === "chat" ? (
          <div
            className={`chat-body ${detailsOpen && selected ? "with-details" : ""}`}
          >
            <section className="chat">
              <div
                className="transcript"
                ref={transcript}
                onScroll={() => {
                  const e = transcript.current!;
                  sticky.current =
                    e.scrollHeight - e.scrollTop - e.clientHeight < 80;
                }}
              >
                {!selected && (
                  <section className="empty">
                    <h2>무엇을 함께할까요?</h2>
                    <p>
                      프로젝트를 선택하고 첫 지시를 입력하세요. 계정과 설정은
                      왼쪽 메뉴에서 바로 관리합니다.
                    </p>
                    <label>
                      프로젝트 경로
                      <input
                        value={cwd}
                        onChange={(e) => setCwd(e.target.value)}
                        placeholder="/Users/ChangeMe/WebstormProjects/…"
                      />
                    </label>
                    <details>
                      <summary>새 작업 설정</summary>
                      <div className="form-grid">
                        <label>
                          종류
                          <select
                            value={kind}
                            onChange={(e) => setKind(e.target.value)}
                          >
                            {["chat", "task", "goal", "group"].map((k) => (
                              <option key={k}>{k}</option>
                            ))}
                          </select>
                        </label>
                        <label>
                          계정
                          <select
                            value={account}
                            onChange={(e) => setAccount(e.target.value)}
                          >
                            <option value="">정책 순서로 자동 선택</option>
                            {(snap.account_names || []).map((n: string) => (
                              <option key={n}>{n}</option>
                            ))}
                          </select>
                        </label>
                        <label>
                          제목
                          <input
                            value={title}
                            onChange={(e) => setTitle(e.target.value)}
                          />
                        </label>
                        <label>
                          모델
                          <input
                            value={model}
                            onChange={(e) => setModel(e.target.value)}
                            placeholder="공식 기본 설정"
                          />
                        </label>
                        <label>
                          추론 강도
                          <select
                            value={effort}
                            onChange={(e) => setEffort(e.target.value)}
                          >
                            <option value="">기본</option>
                            {[
                              "low",
                              "medium",
                              "high",
                              "xhigh",
                              "max",
                              "ultra",
                            ].map((n) => (
                              <option key={n}>{n}</option>
                            ))}
                          </select>
                        </label>
                        <label>
                          상위 그룹
                          <select
                            value={parent}
                            onChange={(e) => setParent(e.target.value)}
                          >
                            <option value="">없음</option>
                            {jobs
                              .filter((j) => j.kind === "group")
                              .map((j) => (
                                <option value={j.id} key={j.id}>
                                  {j.title}
                                </option>
                              ))}
                          </select>
                        </label>
                      </div>
                      <label>
                        완료 검사 · JSON 명령 인자 배열
                        <input
                          value={check}
                          onChange={(e) => setCheck(e.target.value)}
                          placeholder={'["go", "test", "./..."]'}
                        />
                      </label>
                      <label>
                        <span>
                          <input
                            type="checkbox"
                            checked={readOnly}
                            onChange={(e) => setReadOnly(e.target.checked)}
                          />
                          읽기 전용
                        </span>
                      </label>
                      {kind === "goal" && (
                        <>
                          <label>
                            목표
                            <textarea
                              value={goal}
                              onChange={(e) => setGoal(e.target.value)}
                            />
                          </label>
                          <label>
                            토큰 예산
                            <input
                              type="number"
                              min="1"
                              value={budget}
                              onChange={(e) => setBudget(e.target.value)}
                            />
                          </label>
                        </>
                      )}
                    </details>
                  </section>
                )}
                {detail.cursor && (
                  <button onClick={() => void older()}>
                    이전 기록 더 보기
                  </button>
                )}
                {(detail.thread?.turns || []).map((turn: Data) => (
                  <section key={turn.id}>
                    {(turn.items || []).map((item: Data) =>
                      item.type === "userMessage" ? (
                        <article className="message user" key={item.id}>
                          <small>사용자</small>
                          <p>
                            {(item.content || [])
                              .filter((c: Data) => c.type === "text")
                              .map((c: Data) => c.text)
                              .join("\n")}
                          </p>
                        </article>
                      ) : item.type === "agentMessage" ? (
                        <article className="message" key={item.id}>
                          <small>Codex</small>
                          <ReactMarkdown skipHtml>
                            {item.text || ""}
                          </ReactMarkdown>
                        </article>
                      ) : (
                        <details key={item.id}>
                          <summary>
                            {item.type} · {item.status || ""}
                          </summary>
                          <pre>
                            {item.command || item.text || ""}
                            {item.aggregatedOutput
                              ? "\n" + item.aggregatedOutput
                              : ""}
                            {item.changes
                              ?.map((c: Data) => `${c.path}\n${c.diff}`)
                              .join("\n")}
                          </pre>
                        </details>
                      ),
                    )}
                    {turn.error && (
                      <p className="error">{turn.error.message}</p>
                    )}
                  </section>
                ))}
                {job?.last_output && job.state === "running" && (
                  <pre aria-label="진행 중 출력">{job.last_output}</pre>
                )}
                <Requests job={job} run={run} />
              </div>
              <form
                className="composer"
                onSubmit={(e) => {
                  e.preventDefault();
                  void submit();
                }}
              >
                <textarea
                  ref={input}
                  aria-label="메시지"
                  placeholder="메시지를 입력하거나 /로 관리 명령을 선택하세요"
                  value={draft}
                  onChange={(e) => updateDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if (
                      shouldSend({
                        key: e.key,
                        shiftKey: e.shiftKey,
                        isComposing: e.nativeEvent.isComposing,
                        keyCode: e.nativeEvent.keyCode,
                      })
                    ) {
                      e.preventDefault();
                      void submit();
                    }
                  }}
                />
                {draft.startsWith("/") && (
                  <div className="toolbar">
                    {commands
                      .filter((c) => c.name.startsWith(draft.trim()))
                      .map((c) => (
                        <button
                          type="button"
                          key={c.name}
                          onClick={() => {
                            setView(c.view);
                            updateDraft("");
                          }}
                        >
                          {c.name} · {c.label}
                        </button>
                      ))}
                  </div>
                )}
                <div className="toolbar">
                  <button
                    disabled={
                      !!busy ||
                      (!draft.trim() &&
                        (selected !== null || kind !== "group")) ||
                      (!canWrite && !directCommand(draft))
                    }
                  >
                    {!selected && kind === "group" ? "그룹 만들기" : "전송"}
                  </button>
                  {job?.state === "running" && (
                    <>
                      <label>
                        <span>
                          <input
                            type="checkbox"
                            checked={steer}
                            onChange={(e) => setSteer(e.target.checked)}
                          />
                          진행 중 턴에 전달
                        </span>
                      </label>
                      <button type="button" onClick={() => action("jobs.stop")}>
                        중지
                      </button>
                    </>
                  )}
                  <select
                    aria-label="이전 입력"
                    value=""
                    onChange={(e) => updateDraft(e.target.value)}
                  >
                    <option value="">이전 입력</option>
                    {history.map((h, i) => (
                      <option value={h} key={i}>
                        {h.slice(0, 100)}
                      </option>
                    ))}
                  </select>
                  <small>Enter 전송 · Shift+Enter 줄바꿈</small>
                </div>
              </form>
            </section>
            {detailsOpen && selected && (
              <aside className="details">
                <button onClick={() => setDetailsOpen(false)}>상세 닫기</button>
                <h2>{job?.state || selected.state}</h2>
                <p>{selected.cwd}</p>
                <p>
                  계정: {job?.account || selected.account || "미확인"}
                  <br />
                  모델: {job?.model || selected.model || "공식 기본 설정"}
                  <br />
                  작업 토큰:{" "}
                  {job?.token_usage?.total?.totalTokens ??
                    selected.tokens_used ??
                    "미확인"}
                </p>
                {!selected.job_id ? (
                  <button
                    disabled={!canWrite}
                    onClick={() =>
                      void run("jobs.register", {
                        thread_id: selected.thread_id || selected.id,
                      })
                        .then((j) => {
                          choose({
                            ...selected,
                            job_id: j.id,
                            managed: j.managed,
                          });
                        })
                        .catch(() => {})
                    }
                  >
                    관리 등록
                  </button>
                ) : (
                  <>
                    <div className="toolbar">
                      {[
                        ["jobs.manage", "실행권 가져오기"],
                        ["jobs.pause", "일시 중지"],
                        ["jobs.resume", "재개"],
                        ["jobs.complete", "완료"],
                        ["jobs.unregister", "등록 해제"],
                      ].map(([method, label]) => (
                        <button
                          key={method}
                          disabled={!!busy || !canWrite}
                          onClick={() => action(method)}
                        >
                          {label}
                        </button>
                      ))}
                      <button onClick={() => action("terminal.open")}>
                        공식 터미널 열기
                      </button>
                    </div>
                    <label>
                      이 작업의 계정
                      <select
                        value={targetAccount}
                        onChange={(e) => setTargetAccount(e.target.value)}
                      >
                        <option value="">계정 선택</option>
                        {(snap.account_names || []).map((n: string) => (
                          <option key={n}>{n}</option>
                        ))}
                      </select>
                    </label>
                    <button
                      disabled={
                        !targetAccount ||
                        !job?.id ||
                        job?.kind === "group" ||
                        !canWrite ||
                        !!busy
                      }
                      onClick={() =>
                        action("jobs.account", {
                          account: targetAccount,
                          run_id: job?.run_id || "",
                          expected_account: job?.account,
                        })
                      }
                    >
                      계정 변경 요청
                    </button>
                    <p>
                      {job?.target_account
                        ? `변경 요청: ${job.target_account}`
                        : ""}
                    </p>
                    <p className="error">{job?.last_error}</p>
                    {detail.goal && (
                      <section>
                        <h3>목표</h3>
                        <p>{detail.goal.objective}</p>
                        <p>
                          {detail.goal.status} · {detail.goal.tokensUsed} /{" "}
                          {detail.goal.tokenBudget ?? "예산 미설정"} 토큰 ·{" "}
                          {detail.goal.timeUsedSeconds}초
                        </p>
                      </section>
                    )}
                    <details>
                      <summary>목표 편집</summary>
                      <form
                        key={detail.job?.id || "loading-goal"}
                        className="form"
                        onSubmit={(e) => {
                          e.preventDefault();
                          const form = new FormData(e.currentTarget);
                          action("jobs.goal", {
                            text: form.get("objective"),
                            token_budget: form.get("budget")
                              ? Number(form.get("budget"))
                              : undefined,
                            goal_status: form.get("status") || undefined,
                          });
                        }}
                      >
                        <label>
                          목표
                          <textarea
                            name="objective"
                            defaultValue={detail.goal?.objective || ""}
                            required
                          />
                        </label>
                        <label>
                          토큰 예산
                          <input
                            name="budget"
                            type="number"
                            min="1"
                            defaultValue={detail.goal?.tokenBudget || ""}
                          />
                        </label>
                        <label>
                          목표 상태
                          <select name="status" defaultValue="">
                            <option value="">
                              현재 상태 유지 ·{" "}
                              {detail.goal?.status || "새 목표"}
                            </option>
                            <option value="active">진행</option>
                            <option value="paused">일시 중지</option>
                            <option value="blocked">진행 불가</option>
                            <option value="usageLimited">사용 한도 제한</option>
                            <option value="budgetLimited">
                              목표 예산 제한
                            </option>
                            <option value="complete">완료</option>
                          </select>
                        </label>
                        <button>목표 저장</button>
                      </form>
                    </details>
                    <p className="muted">
                      외부 GUI가 실행 중인 대화는 해당 writer가 해제된 뒤 재개할
                      수 있습니다. 목표 분기는 예산 승계 검증 전까지 제공하지
                      않습니다.
                    </p>
                  </>
                )}
              </aside>
            )}
          </div>
        ) : (
          <div className="content">
            {view === "accounts" && (
              <Accounts
                snapshot={snap}
                generation={caps.generation}
                run={run}
                login={login}
                setLogin={setLogin}
              />
            )}
            {view === "settings" && <Settings run={run} />}
            {view === "logs" && (
              <Logs jobs={jobs} selected={selected?.job_id} />
            )}
            {view === "usage" && (
              <>
                <h2>계정 전체 한도</h2>
                <table>
                  <thead>
                    <tr>
                      <th>계정</th>
                      <th>단기 잔여</th>
                      <th>장기 잔여</th>
                      <th>관측</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(snap.state?.accounts || []).map((a: Data) => {
                      const b = a.rateLimitsByLimitId?.codex || a.rateLimits;
                      return (
                        <tr key={a.account}>
                          <td>{a.account}</td>
                          <td>{remaining(b?.primary)}</td>
                          <td>{remaining(b?.secondary)}</td>
                          <td>{new Date(a.checked_at).toLocaleString()}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
                <p>
                  작업별 토큰과 실행 시간은 작업 상세 및 사용 기록에서
                  확인합니다.
                </p>
              </>
            )}
            {view === "manage" && (
              <>
                <h2>관리 작업</h2>
                {jobs.map((j) => (
                  <p key={j.id}>
                    <button
                      onClick={() =>
                        choose({
                          id: j.thread_id || j.id,
                          thread_id: j.thread_id,
                          job_id: j.id,
                          title: j.title,
                          cwd: j.cwd,
                          state: j.state,
                          kind: j.kind,
                          managed: j.managed,
                          archived: false,
                        })
                      }
                    >
                      {j.title || j.id}
                    </button>{" "}
                    · {j.account} · {j.state}
                  </p>
                ))}
              </>
            )}
            {view === "manager" && (
              <form
                className="form"
                onSubmit={(e) => {
                  e.preventDefault();
                  void run("manager.invoke", {
                    id: managerAll ? "" : selected?.job_id || "",
                    text: managerText,
                  })
                    .then(() => setManagerText(""))
                    .catch(() => {});
                }}
              >
                <h2>관리자에게 요청 · 모델 호출</h2>
                <p>
                  관리자 계정 {snap.manager_account || "미설정"}으로 명시적인
                  모델 요청을 보냅니다.
                </p>
                <label>
                  <span>
                    <input
                      type="checkbox"
                      checked={managerAll}
                      onChange={(e) => setManagerAll(e.target.checked)}
                    />
                    전체 작업 범위
                  </span>
                </label>
                <label>
                  관리자 지시
                  <textarea
                    required
                    value={managerText}
                    onChange={(e) => setManagerText(e.target.value)}
                  />
                </label>
                <button
                  disabled={
                    !canWrite || !!busy || (!managerAll && !selected?.job_id)
                  }
                >
                  관리자 모델 호출
                </button>
              </form>
            )}
            {view === "status" && (
              <>
                <h2>연결 상태</h2>
                <p>
                  제품 {caps.version || "확인 중"} · 공식 엔진{" "}
                  {caps.engine || "미설정"} · 브리지 계약 {caps.protocolVersion}
                </p>
                <p>
                  관리 서비스 {snap.connected ? "연결됨" : "중지됨"} · 저장소
                  변경 번호 {snap.state?.sequence ?? "없음"}
                </p>
                <p>
                  웹 중지: <code>codexm web stop</code> · 개발 세션:{" "}
                  <code>tmux attach -t codexm-web-dev</code>
                </p>
                <button
                  onClick={() =>
                    void run("service.status")
                      .then(setOperationResult)
                      .catch(() => {})
                  }
                >
                  서비스 진단
                </button>
                <h3>결과 확인이 필요한 요청</h3>
                {uncertain.map((p) => (
                  <p key={p.id}>
                    {p.method} · {p.id}{" "}
                    <button
                      onClick={() =>
                        void call("operation.read", { operation_id: p.id })
                          .then(setOperationResult)
                          .catch((e) => setNotice(e.reason || e.message))
                      }
                    >
                      결과 조회
                    </button>
                    <button
                      onClick={() =>
                        recordOperations(uncertain.filter((x) => x.id !== p.id))
                      }
                    >
                      대화 확인 후 목록에서 제거
                    </button>
                  </p>
                ))}
                {operationResult && (
                  <pre>{JSON.stringify(operationResult, null, 2)}</pre>
                )}
                <h3>복구 대기</h3>
                {(snap.state?.actions || []).map((a: Data) => (
                  <p key={a.id}>
                    {a.kind} · {a.state} · {a.error}
                    {a.state === "outcome_unknown" && (
                      <>
                        <button
                          onClick={() =>
                            void run("actions.resolve", {
                              action_id: a.id,
                              decision: "done",
                            }).catch(() => {})
                          }
                        >
                          이력 확인 후 완료로 기록
                        </button>
                        <button
                          onClick={() =>
                            void run("actions.resolve", {
                              action_id: a.id,
                              decision: "cancelled",
                            }).catch(() => {})
                          }
                        >
                          이력 확인 후 취소로 기록
                        </button>
                      </>
                    )}
                  </p>
                ))}
              </>
            )}
          </div>
        )}
      </section>
    </main>
  );
}
