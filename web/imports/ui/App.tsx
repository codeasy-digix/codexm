import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { authenticate, call, exchange, login, reconnect, useAccess, useStateData } from "../../client/data";
import { chatIdFromPath, chatPath, chatTitle, inheritTurnSelection, mergeLiveTurn, type Catalog, type ChatActivity, type ChatThread, type TurnSelection } from "../contracts/web";
import { AccountLimits } from "./AccountLimits";
import { ChatActivityLabel } from "./ChatActivity";
import { ChatMessages } from "./ChatMessages";
import { ChatFiles, useChatFiles } from "./ChatFiles";
import { ChatRequest } from "./ChatRequest";
import { Composer } from "./Composer";
import { GUIQuitDialog } from "./GUIQuitDialog";
import { Modal } from "./Modal";
import { TurnSettings } from "./TurnSettings";
import { readChatShortcuts } from "./chat-shortcuts";
import { useCatalog, useChatActivity, useChatHistory, useChatModels } from "./chat-hooks";
import { SidebarChat } from "./SidebarChat";
import { WorkspaceSettings } from "./WorkspaceSettings";
import { FolderPicker } from "./FolderPicker";
import { GoalIndicator } from "./GoalIndicator";
import { useTheme } from "./theme";
import "./styles/native.css";
import "./styles/workspace.css";

type Dialog = {kind:"new"|"project"|"settings"|"gui-quit"}|{kind:"move"|"archive"|"delete"|"files";chat:ChatThread};
type Section = "chats"|"pinned"|"archived";

export function App() {
  const access=useAccess(),data=useStateData();
  const [theme, setTheme] = useTheme();
  const [pinPending, setPinPending] = useState<string[]>([]);
  const ready = access.status === "authenticated";
  const [account,setAccount]=useState("default"),[section,setSection]=useState<Section>("chats"),[project,setProject]=useState("");
  const [path, setPath] = useState(() => location.pathname);
  const [opened,setOpened]=useState<ChatThread|null>(null);
  const [openError, setOpenError] = useState<string | null>(null);
  const [openAttempt, setOpenAttempt] = useState(0);
  const threadId = chatIdFromPath(path);
  const selected = ready && opened?.id === threadId ? opened : null;
  const [query,setQuery]=useState(""),[sidebar,setSidebar]=useState(false);
  const searchInput = useRef<HTMLInputElement>(null);
  const [turnSelections, setTurnSelections] = useState<Record<string, TurnSelection>>({});
  const [shortcuts, setShortcuts] = useState(readChatShortcuts);
  const [dialog,setDialog]=useState<Dialog|null>(null),[menu,setMenu]=useState(false),[error,setError]=useState(""),[notice,setNotice]=useState("");
  const [formName,setFormName]=useState(""),[formPath,setFormPath]=useState(""),[formProject,setFormProject]=useState(""),[formBusy,setFormBusy]=useState(false);
  const [formAccount,setFormAccount]=useState("default");
  const [accountChoice, setAccountChoice] = useState<{ thread: string; name: string }>();
  const catalogState=useCatalog(ready,query,project,section==="archived",threadId,access.online);
  const {catalog}=catalogState;
  const projects=[...(catalog?.projects||[]),...(catalog?.otherProjects||[])];
  const selectedProject = projects.find(p => p.id === project);
  const newChatReady = !!catalog && (!project || !!selectedProject);
  const chatState=useChatHistory(ready, selected, account, access.online);
  const history=chatState.history?.thread.id===selected?.id?chatState.history:undefined;
  const fileState = useChatFiles(ready, access.online, selected?.id ?? "", history?.turns, dialog?.kind === "files" && dialog.chat.id === selected?.id);
  const activity = useChatActivity(ready, selected?.id || "", account);
  const pending = activity.pending ?? history?.pending ?? [];
  const blocking = pending.find(request => request.params.isBlocking !== false);
  const externalDelivery = history?.thread.externalOwner ? history.delivery : undefined;
  const externalState = externalDelivery?.state === "ready" ? "running" :
    externalDelivery?.state === "idle" || externalDelivery?.state === "waiting" ? externalDelivery.state : undefined;
  const openActivity: ChatActivity | undefined = blocking ? {
    source: "web", state: "waiting", turnId: String(blocking.params.turnId || ""),
  } : externalState ? { source: "external", state: externalState, turnId: externalDelivery?.turnId } :
    (selected && activity.threads?.[selected.id]) ||
    (history?.thread.externalOwner ? { source: "external", state: "unknown" } :
      history?.thread.status.type === "active" ? {
        source: "web", state: history.pending.length ? "waiting" : "running",
        turnId: history.turns.at(-1)?.id,
      } : undefined);
  const turns = useMemo(() => mergeLiveTurn(history?.turns ?? [], activity.turn), [history?.turns, activity.turn]);
  const liveTurnId = openActivity && openActivity.state !== "idle" && openActivity.state !== "unknown" ? openActivity.turnId : undefined;
  const latest = turns.at(-1);
  const suggestion = !liveTurnId && !blocking && !history?.thread.externalOwner &&
    latest?.status === "completed" && activity.prediction?.sourceTurnId === latest.id ? activity.prediction.text : undefined;
  const projectNames = new Map<string, number>();
  for (const p of projects) projectNames.set(p.name, (projectNames.get(p.name) ?? 0) + 1);
  const scroll=useRef<HTMLDivElement>(null),stick=useRef(true),anchor=useRef<{height:number;top:number;thread:string;revision:number}|null>(null);
  const viewed = useRef(new Map<string, { chat: ChatThread; top: number; bottom: boolean }>());
  const viewport = useRef({ thread: "", restored: false });
  const hasHistory = !!history;
  const first=new Date().getHours()<12?"무엇부터 시작할까요?":"어떤 작업을 이어갈까요?";
  const accounts: Catalog["accounts"] = catalog?.accounts || data.snapshot?.accounts || [];
  const defaultAccount = catalog?.defaultAccount || accounts.find((candidate: {name: string; default?: boolean}) => candidate.default)?.name;
  const defaultLabel = defaultAccount ? `기본 계정 · ${defaultAccount}` : "기본 계정";
  const executionAccount = history?.executionAccount;
  const pendingAccountChange = !!executionAccount && executionAccount !== (account === "default" ? defaultAccount || account : account);
  const switchAccount = accountChoice?.thread === selected?.id && accountChoice?.name === account;
  const chat=selected&&history?{
    ...history.thread,
    name: history.thread.name || selected.name,
    preview: history.thread.preview || selected.preview,
    model: history.thread.model || selected.model,
    reasoningEffort: history.thread.reasoningEffort || selected.reasoningEffort,
  }:selected;
  const composerKey = `${selected?.id}:${account}`;
  const selection = turnSelections[composerKey] ?? inheritTurnSelection;
  const isArchived=history?.thread.archived??selected?.archived??false;
  const modelState = useChatModels(ready && ((!!selected && !isArchived) || dialog?.kind === "settings"), account, history?.loginGeneration);
  const displayChats=section==="pinned"?(catalog?.pinned||[]).filter(c=>`${chatTitle(c)} ${c.cwd}`.toLowerCase().includes(query.toLowerCase())):catalog?.threads||[];

  useEffect(()=>{
    if (access.status === "anonymous") {
      setOpened(null);setOpenError(null);setDialog(null);setQuery("");setSidebar(false);setTurnSelections({});setAccount("default");
      setAccountChoice(undefined);
    }
    if (!ready) viewed.current.clear();
  },[access.status, ready]);
  useEffect(() => {
    const onPopState = () => {
      setPath(location.pathname);
      const previous = viewed.current.get(chatIdFromPath(location.pathname));
      setOpened(previous?.chat ?? null);
      setAccountChoice(undefined);
      if (previous) setAccount(previous.chat.account || "default");
      setOpenError(null);
      setSidebar(false);
      setError("");
      setNotice("");
    };
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);
  useEffect(() => {
    if (!access.online || !threadId || selected) return;
    let current = true;
    setOpenError(null);
    void call<ChatThread | null>("web.thread", { thread_id: threadId }).then(chat => {
      if (!current) return;
      if (!chat) {
        setOpenError("이 주소의 채팅을 찾을 수 없습니다.");
        return;
      }
      setOpened(chat);
      setAccount(chat.account || "default");
      if (chat.archived) { setSection("archived"); setProject(""); }
    }).catch(() => {
      if (current) setOpenError("채팅을 불러오지 못했습니다.");
    });
    return () => { current = false; };
  }, [access.online, threadId, selected, openAttempt]);
  useEffect(() => { setMenu(false); }, [selected?.id, account]);
  useLayoutEffect(()=>{
    const thread = selected?.id ?? "";
    if (viewport.current.thread !== thread) {
      viewport.current = { thread, restored: false };
      anchor.current = null;
    }
    const node = scroll.current;
    if (!node || !thread || !hasHistory) return;
    if (!viewport.current.restored) {
      const saved = viewed.current.get(thread);
      stick.current = saved?.bottom ?? true;
      node.scrollTop = stick.current ? node.scrollHeight : saved?.top ?? 0;
      viewport.current.restored = true;
    } else if (anchor.current?.thread === thread && anchor.current.revision === chatState.olderRevision) {
      node.scrollTop = node.scrollHeight - anchor.current.height + anchor.current.top;
      anchor.current = null;
    } else if (stick.current && !anchor.current) node.scrollTop = node.scrollHeight;
  },[turns, selected?.id, hasHistory, chatState.olderRevision]);
  useLayoutEffect(() => {
    if (selected && !viewed.current.has(selected.id)) rememberPosition(selected, 0, true);
  }, [selected]);
  useEffect(()=>{const key=(e:KeyboardEvent)=>{if(e.key==="Escape"){setSidebar(false);setMenu(false);}};window.addEventListener("keydown",key);return()=>window.removeEventListener("keydown",key);},[]);

  function navigate(id: string, replace = false) {
    const next = chatPath(id);
    if (location.pathname !== next) window.history[replace ? "replaceState" : "pushState"](null, "", next);
    setPath(next);
    setOpenError(null);
  }
  function choose(next:ChatThread) {
    navigate(next.id);
    setOpened(next);setAccount(next.account&&accounts.some((a:any)=>a.name===next.account)?next.account:"default");
    setAccountChoice(undefined);
    setSidebar(false);setError("");setNotice("");
  }
  function filter(next:Section,id="") {setSection(next);setProject(id);}
  function openDialog(next:Dialog) {
    setError("");setMenu(false);setFormName("");
    const target=next.kind==="new"?selectedProject:undefined;
    if (next.kind === "new") setFormAccount(account);
    setFormPath(target?.roots[0]?.path||catalog?.defaultCwd||"");
    setFormProject("chat"in next?next.chat.projectId||"":target?.id||"");setDialog(next);
  }
  async function mutate(method:string,params:Record<string,unknown>) {
    setError("");const result=await call(method,{account,...params},crypto.randomUUID());
    void catalogState.refresh(undefined,true);return result;
  }
  async function submitDialog(e:React.FormEvent) {
    e.preventDefault();if(!dialog||formBusy)return;setFormBusy(true);setError("");
    try {
      if(dialog.kind==="new") {
        const result=await mutate("web.create",{cwd:formPath,project_id:formProject,account:formAccount});choose(result.thread);
      } else if(dialog.kind==="project") {
        const result=await mutate("web.projectCreate",{name:formName,cwd:formPath});filter("chats",result.project.id);setNotice("프로젝트를 추가했습니다.");
      } else if(dialog.kind==="move") {
        await mutate("web.move",{thread_id:dialog.chat.id,project_id:formProject,account:dialog.chat.account || "default"});
        setNotice("프로젝트를 옮겼습니다.");void chatState.refresh();
      } else if(dialog.kind==="archive") {
        await mutate("web.archive",{thread_id:dialog.chat.id,archived:!dialog.chat.archived,account:dialog.chat.account || "default"});
        if (dialog.chat.id === selected?.id) { navigate("", true); setOpened(null); }
        setNotice(dialog.chat.archived?"보관을 해제했습니다.":"채팅을 보관했습니다.");
      } else if(dialog.kind==="delete") {
        await mutate("web.delete",{thread_id:dialog.chat.id});
        chatState.forget(dialog.chat.id);
        viewed.current.delete(dialog.chat.id);
        navigate("",true);setOpened(null);setNotice("채팅을 영구 삭제했습니다.");
      }
      setDialog(null);
    }catch(e){setError((e as Error).message);}finally{setFormBusy(false);}
  }
  async function pin(target: ChatThread) {
    if (pinPending.includes(target.id) || (target.pinned && section !== "pinned")) return;
    setPinPending(current => [...current, target.id]);
    try {
      await mutate("web.pin", { thread_id: target.id, pinned: !target.pinned, account: target.account || "default" });
      setNotice(target.pinned ? "채팅 고정을 해제했습니다." : "채팅을 고정했습니다.");
    } catch (reason) { setError((reason as Error).message); }
    finally { setPinPending(current => current.filter(id => id !== target.id)); }
  }
  async function loadOlder() {
    const node=scroll.current;if(!node||!selected||anchor.current||!history?.nextCursor)return;
    const request = {height:node.scrollHeight,top:node.scrollTop,thread:selected.id,revision:chatState.olderRevision+1};
    anchor.current = request;
    const loaded = await chatState.loadOlder();
    if (!loaded && anchor.current === request) anchor.current = null;
  }

  function rememberPosition(chat: ChatThread, top: number, bottom: boolean) {
    viewed.current.delete(chat.id);
    viewed.current.set(chat.id, { chat, top, bottom });
    if (viewed.current.size > 12) viewed.current.delete(viewed.current.keys().next().value!);
  }
  function scrolled(node: HTMLDivElement) {
    if (!selected || !hasHistory || !viewport.current.restored) return;
    stick.current = node.scrollHeight - node.clientHeight - node.scrollTop < 100;
    rememberPosition(selected, node.scrollTop, stick.current);
    if (node.scrollTop < 90) void loadOlder();
  }

  if (access.status === "anonymous") return <Login passwordMode={access.password} message={access.message} />;
  if (!ready) return <main className="login">
    <p role={access.status === "unavailable" ? "alert" : "status"}>{access.message || "로그인 상태를 확인하고 있습니다…"}</p>
    {access.status === "unavailable" && <button type="button" onClick={() => void authenticate()}>다시 확인</button>}
  </main>;
  return <div className="chat-app">
    {sidebar&&<button className="sidebar-backdrop" aria-label="메뉴 닫기" onClick={()=>setSidebar(false)}/>}
    <aside className={`sidebar ${sidebar?"is-open":""}`} aria-label="탐색">
      <div className="brand">
        <span className="brand-icon">C</span><strong>CodexM</strong>
        {catalog?.guiQuitAvailable && <button className="gui-quit-button" type="button" disabled={!access.online}
          onClick={() => { setSidebar(false); openDialog({ kind: "gui-quit" }); }}>GUI 종료</button>}
        <span>이 머신</span>
        <button className="mobile-close" aria-label="메뉴 닫기" onClick={()=>setSidebar(false)}>×</button>
      </div>
      <button className="new-chat" aria-label="새 채팅" disabled={!newChatReady} onClick={()=>openDialog({kind:"new"})}><span aria-hidden="true">＋</span>새 채팅</button>
      <div className="sidebar-search">
        <input ref={searchInput} aria-label="채팅 검색" placeholder="채팅 검색" value={query} onChange={e=>setQuery(e.target.value)}/>
        {query && <button className="search-clear" type="button" aria-label="검색어 지우기" title="검색어 지우기"
          onClick={() => { setQuery(""); searchInput.current?.focus(); }}>×</button>}
      </div>
      <nav className="sections" aria-label="채팅 분류">
        <button aria-pressed={section==="chats"} onClick={()=>filter("chats")}><span aria-hidden="true">◷</span>전체 채팅</button>
        <button aria-label="고정" aria-pressed={section==="pinned"} onClick={()=>filter("pinned")}><span aria-hidden="true">◇</span>고정 <small>{catalog?.pinned.length||0}</small></button>
        <button aria-pressed={section==="archived"} onClick={()=>filter("archived")}><span aria-hidden="true">▤</span>보관함</button>
      </nav>
      {section === "chats" && <div className="project-filter">
        <select aria-label="프로젝트 필터" value={project} disabled={!catalog} onChange={e => setProject(e.target.value)}>
          <option value="">전체 프로젝트</option>
          {projects.map(p => <option key={p.id} value={p.id}>
            {p.name}{(projectNames.get(p.name) ?? 0) > 1 && p.roots[0] ? ` · ${p.roots[0].path}` : ""}
            {catalog?.otherProjects?.some(other => other.id === p.id) ? " · GUI 목록 외" : ""}
          </option>)}
        </select>
        <button aria-label="프로젝트 추가" onClick={() => openDialog({kind:"project"})}>＋</button>
      </div>}
      <div className="list-heading"><span>{section==="pinned"?"고정한 채팅":section==="archived"?"보관된 채팅":project?`${selectedProject?.name || "프로젝트"} 채팅`:"전체 채팅"}</span>{catalogState.loading&&<small>갱신 중</small>}</div>
      <div className="chat-list" aria-label="채팅 목록">{displayChats.map(chat =>
        <SidebarChat key={chat.id} chat={chat} selected={chat.id === threadId} pinnedTab={section === "pinned"}
          busy={pinPending.includes(chat.id)} known={activity.writersKnown}
          activity={chat.id === selected?.id ? openActivity : activity.threads?.[chat.id]}
          onOpen={() => chat.id === selected?.id ? setSidebar(false) : choose(chat)}
          onPin={() => void pin(chat)}
          onArchive={() => { setSidebar(false); openDialog({ kind: "archive", chat }); }}
          onMove={() => { setSidebar(false); openDialog({ kind: "move", chat }); }} />)}
        {!displayChats.length&&catalog&&!catalogState.loading&&<p className="muted empty-list">표시할 채팅이 없습니다.</p>}
        {catalogState.error&&<p role="alert" className="inline-error">{catalogState.error}</p>}
        {section!=="pinned"&&catalog?.nextCursor&&<button className="load-more" disabled={catalogState.loading} onClick={()=>void catalogState.refresh(catalog.nextCursor)}>채팅 더 보기</button>}
      </div>
      <div className="sidebar-footer"><button onClick={() => { setSidebar(false); openDialog({ kind: "settings" }); }}>설정</button></div>
    </aside>
    <main className="conversation" data-thread-id={selected?.id||""}>
      <header className="chat-header"><button className="menu-toggle" aria-label="메뉴 열기" onClick={()=>setSidebar(true)}>☰</button><div className="chat-heading"><h1>{chat?chatTitle(chat):"새로운 작업"}</h1><small>{chat?.cwd||"로컬 채팅 · 내 프로젝트"}</small></div>
        <div className={`chat-header-controls${chat && !isArchived ? " has-turn-settings" : ""}`}>
          <div className="account-header">
            <div className="account-actions-row">
              {(!threadId || selected) && <AccountLimits key={account} account={account} />}
              <label className="account-select">계정
                <select aria-label="이번에 사용할 계정" title={account === "default" ? defaultLabel : account} value={account} disabled={!!threadId && !selected} onChange={e => {
                  const name = e.target.value;
                  setAccountChoice(selected ? { thread: selected.id, name } : undefined);
                  setAccount(name);
                }}><option value="default">{defaultLabel}</option>{accounts.map((a:any)=><option key={a.name} value={a.name}>{a.name}</option>)}</select>
              </label>
            </div>
            {executionAccount && <small className="default-account-name">현재 실행 {executionAccount === "default" ? "기본 계정" : executionAccount}</small>}
            {account !== "default" && defaultAccount && <small className="default-account-name" title={defaultLabel}>{defaultLabel}</small>}
          </div>
          {chat && !isArchived && <TurnSettings
            key={composerKey}
            chat={chat}
            models={modelState.models}
            loading={modelState.loading}
            modelError={modelState.error}
            retryModels={modelState.retry}
            selection={selection}
            onSelectionChange={value => setTurnSelections(current => ({ ...current, [composerKey]: value }))}
            shortcuts={shortcuts}
            shortcutsEnabled={!dialog && !sidebar && !menu}
            deferred={!!((!pendingAccountChange && (activity.accountTurn?.status === "inProgress" || history?.thread.status.type === "active")) || history?.thread.externalOwner)}
          />}
        </div>
        {chat&&<div className="chat-menu"><button aria-label="채팅 메뉴" aria-expanded={menu} onClick={()=>setMenu(!menu)}>···</button>{menu&&<div className="popover" role="menu"><button role="menuitem" onClick={()=>openDialog({kind:"files",chat})}>채팅 파일</button><button role="menuitem" onClick={()=>openDialog({kind:"move",chat:{...chat}})}>프로젝트로 이동</button><button role="menuitem" className="danger" onClick={()=>openDialog({kind:"delete",chat:{...chat}})}>영구 삭제</button></div>}</div>}
      </header>
      {!access.online && <div className="banner connection-banner" role="status">
        {access.message || "서버 연결을 복구하는 중…"}
        <button className="text-button" type="button" onClick={reconnect}>다시 연결</button>
      </div>}
      {selected && history?.goal && <GoalIndicator key={"goal:" + selected.id} goal={history.goal}
        thread={selected.id} account={account} canControl={!!history.webOnly && !isArchived}
        online={access.online} switchAccount={switchAccount} onChanged={() => void chatState.refresh()} />}
      {!dialog&&error&&<div role="alert" className="banner error-banner">{error}</div>}{notice&&<div role="status" className="banner notice-banner">{notice}<button aria-label="알림 닫기" onClick={()=>setNotice("")}>×</button></div>}
      <div className="message-scroll" ref={scroll} aria-label="대화 내용" onScroll={e=>scrolled(e.currentTarget)}>
        {selected?<div className="message-inner">
          {history?.nextCursor&&<button className="older-button" disabled={chatState.loadingOlder} onClick={()=>void loadOlder()}>{chatState.loadingOlder?"이전 메시지를 불러오는 중…":"이전 메시지 불러오기"}</button>}
          {chatState.loading&&<p className="loading-message" role="status">최근 대화를 불러오는 중…</p>}
          {chatState.error&&<p role="alert" className="inline-error">{chatState.error}</p>}
          {(!!turns.length || !!history?.relayedInputs?.length) && <ChatMessages thread={selected.id} turns={turns} account={account}
            external={history?.thread.externalOwner} liveTurnId={liveTurnId}
            relayedInputs={history?.relayedInputs} hasOlder={!!history?.nextCursor} cwd={selected.cwd} files={fileState.files} refs={fileState.refs}/>}
          {history&&!turns.length&&!history.relayedInputs?.length&&<div className="empty-conversation"><h2>{isArchived?"보관된 대화입니다":"표시할 메시지가 없습니다"}</h2><p>{isArchived?"현재 Codex 원본에서 조회되는 메시지가 없습니다.":"요청을 입력하면 이 대화에서 작업을 이어갑니다."}</p></div>}
        </div>:path !== "/" ? <div className="message-inner">
          {(openError || !threadId)
            ? <div className="empty-conversation" role="alert">
              <p>{openError || "채팅 주소가 올바르지 않습니다."}</p>
              {!!threadId && <button className="text-button" type="button" onClick={() => setOpenAttempt(attempt => attempt + 1)}>다시 불러오기</button>}
              <a href="/" onClick={event => {event.preventDefault();navigate("",true);}}>전체 채팅</a>
            </div>
            : <p className="loading-message" role="status">채팅을 불러오는 중…</p>}
        </div>:<div className="welcome"><span className="welcome-mark">C</span><p className="eyebrow">CODEXM · LOCAL WORKSPACE</p><h2>{first}</h2><p>최근 채팅을 이어가거나,<br/>프로젝트에서 새 작업을 시작하세요.</p><button className="primary" disabled={!newChatReady} onClick={()=>openDialog({kind:"new"})}>＋ 새 채팅 시작</button></div>}
      </div>
      {chat && !isArchived && <ChatActivityLabel activity={openActivity} known={activity.writersKnown} lastTurn={turns.at(-1)} footer />}
      {selected && pending.length > 0 && <div className="pending-requests" aria-live="polite">
        {pending.map(request => <ChatRequest key={request.request_id} request={request} thread={selected.id}
          onDone={() => void chatState.refresh()} />)}
      </div>}
      {selected && openActivity?.source === "external" && openActivity.state === "waiting" && !pending.length &&
        <p className="external-request-note" role="status">이 채팅을 열어 둔 Codex 앱이나 CLI에서 응답해 주세요.</p>}
      {chat&&!isArchived&&<Composer
        key={chat.id}
        chat={chat}
        online={access.online}
        account={account}
        history={history}
        liveTurn={history?.webOnly ? activity.turn : activity.accountTurn}
        pendingAccountChange={pendingAccountChange}
        switchAccount={switchAccount}
        suggestion={suggestion}
        models={modelState.models}
        selection={selection}
        onSent={id => {
          setAccountChoice(undefined);
          stick.current = true;
          if (id && id !== chat.id) {
            navigate(id, true);
          } else {
            void chatState.refresh();
          }
          void catalogState.refresh();
        }}
      />}
      {selected&&isArchived&&<div className="archived-note">보관된 채팅입니다.<button onClick={()=>openDialog({kind:"archive",chat:{...chat!,archived:true}})}>보관 해제</button></div>}
    </main>
    {dialog?.kind === "files" ? dialog.chat.id === selected?.id && <ChatFiles
      key={dialog.chat.id} thread={dialog.chat.id} files={fileState.files} loading={fileState.loading}
      error={fileState.error} onRegistered={fileState.registered} onClose={() => setDialog(null)} />
      : dialog?.kind === "gui-quit" ? <GUIQuitDialog onClose={() => setDialog(null)} onStopped={() => {
      setDialog(null);
      setNotice("Codex GUI를 종료했습니다. 채팅과 계정을 선택해 작업을 이어갈 수 있습니다.");
      void chatState.refresh();
      void catalogState.refresh();
    }} /> : dialog?.kind === "settings" ? <WorkspaceSettings theme={theme} onTheme={setTheme}
      thread={selected?.id} accounts={accounts} onClose={() => setDialog(null)}
      shortcuts={{
        bindings: shortcuts, models: modelState.models, modelsLoading: modelState.loading,
        modelError: modelState.error, retryModels: modelState.retry, onSave: setShortcuts,
      }} /> : dialog && <Modal title={dialog.kind === "new" ? "새 채팅" : dialog.kind === "project" ? "프로젝트 추가" :
      dialog.kind === "move" ? "프로젝트로 이동" : dialog.kind === "archive" ? (dialog.chat.archived ? "보관 해제" : "채팅 보관") : "채팅 영구 삭제"}
      onClose={() => { if (!formBusy) setDialog(null); }}>
      <form className="dialog-form" onSubmit={e=>void submitDialog(e)}>
        {"chat"in dialog&&<p className="dialog-chat-title">{chatTitle(dialog.chat)}</p>}
        {(dialog.kind==="new"||dialog.kind==="move")&&<label>프로젝트<select aria-label="프로젝트" value={formProject} onChange={e=>{setFormProject(e.target.value);const chosen=projects.find(p=>p.id===e.target.value);if(chosen?.roots[0])setFormPath(chosen.roots[0].path);}}><option value="">프로젝트 없음</option>{projects.map(p=><option key={p.id} value={p.id}>{p.name}</option>)}</select></label>}
        {dialog.kind==="new"&&<label>채팅을 시작할 계정<select value={formAccount} onChange={e=>setFormAccount(e.target.value)}><option value="default">{defaultLabel}</option>{accounts.map(candidate=><option key={candidate.name} value={candidate.name}>{candidate.name}</option>)}</select></label>}
        {dialog.kind==="project"&&<label>프로젝트 이름<input value={formName} onChange={e=>setFormName(e.target.value)} placeholder="폴더 이름 사용" maxLength={200}/></label>}
        {(dialog.kind==="new"||dialog.kind==="project") && <FolderPicker value={formPath} onChange={setFormPath} />}
        {dialog.kind==="move"&&<p className="muted">채팅의 프로젝트 소속을 변경합니다. 작업 폴더와 파일을 옮기지는 않습니다.</p>}
        {dialog.kind==="archive"&&<p>{dialog.chat.archived?"채팅을 일반 목록으로 복원합니다.":"진행 중인 작업과 하위 채팅이 중단될 수 있습니다. 채팅을 보관할까요?"}</p>}
        {dialog.kind==="delete"&&<p className="danger">이 채팅과 하위 대화를 영구 삭제합니다. 되돌릴 수 없습니다.</p>}
        {error&&<p role="alert" className="inline-error">{error}</p>}
        <div className="button-row"><button type="button" onClick={()=>setDialog(null)} disabled={formBusy}>취소</button><button className={dialog.kind==="delete"?"danger-button":"primary"} type="submit" disabled={formBusy}>{formBusy?"처리 중…":dialog.kind==="delete"?"영구 삭제":dialog.kind==="new"?"채팅 만들기":dialog.kind==="project"?"프로젝트 추가":"적용"}</button></div>
      </form>
    </Modal>}
  </div>;
}

function Login({ passwordMode, message }: { passwordMode: boolean; message: string }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const secretInput = useRef<HTMLInputElement>(null);

  useEffect(() => {
    // Chrome uses macOS Secure Input while a password field has focus.
    const releaseFocus = () => secretInput.current?.blur();
    const releaseWhenHidden = () => { if (document.hidden) releaseFocus(); };
    window.addEventListener("blur", releaseFocus);
    document.addEventListener("visibilitychange", releaseWhenHidden);
    return () => {
      window.removeEventListener("blur", releaseFocus);
      document.removeEventListener("visibilitychange", releaseWhenHidden);
    };
  }, []);

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    secretInput.current?.blur();
    setBusy(true);
    setError("");
    void (passwordMode ? login(username, password) : exchange(token))
      .catch(error => setError(error.message))
      .finally(() => setBusy(false));
  }

  return <main className="login">
    <div className="brand-icon">C</div><p className="eyebrow">CODEXM</p>
    <h1>내 작업에 연결하세요</h1><p>이 머신의 채팅과 프로젝트를<br/>브라우저에서 이어갑니다.</p>
    <form onSubmit={submit}>
      {passwordMode ? <>
        <label>아이디<input autoComplete="username" value={username} onChange={e => setUsername(e.target.value)} required /></label>
        <label>비밀번호<input ref={secretInput} type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} required /></label>
      </> : <label>로컬 접속 코드<input ref={secretInput} type="password" value={token} onChange={e => setToken(e.target.value)} required /></label>}
      <button className="primary" disabled={busy}>{busy ? "연결 중…" : passwordMode ? "로그인" : "연결"}</button>
    </form>
    {(error || message) && <p role="alert">{error || message}</p>}
  </main>;
}
