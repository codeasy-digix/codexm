import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { authenticate, call, exchange, login, logout, useAccess, useStateData } from "../../client/data";
import { chatIdFromPath, chatPath, chatTitle, inheritTurnSelection, mergeLiveTurn, type ChatActivity, type ChatThread, type TurnSelection } from "../contracts/web";
import { AccountLimits } from "./AccountLimits";
import { Accounts } from "./Accounts";
import { ChatActivityLabel } from "./ChatActivity";
import { Approval, ChatMessages } from "./ChatMessages";
import { Composer } from "./Composer";
import { Modal } from "./Modal";
import { ShortcutSettings } from "./ShortcutSettings";
import { TurnSettings } from "./TurnSettings";
import { readChatShortcuts } from "./chat-shortcuts";
import { useCatalog, useChatActivity, useChatHistory, useChatModels } from "./chat-hooks";
import "./styles/native.css";

type Dialog = {kind:"new"|"project"|"accounts"|"settings"}|{kind:"move"|"archive"|"delete";chat:ChatThread};
type Section = "chats"|"pinned"|"archived";

export function App() {
  const access=useAccess(),data=useStateData();
  const ready = access.status === "authenticated";
  const [account,setAccount]=useState("default"),[section,setSection]=useState<Section>("chats"),[project,setProject]=useState("");
  const [path, setPath] = useState(() => location.pathname);
  const [opened,setOpened]=useState<ChatThread|null>(null);
  const [openError, setOpenError] = useState<string | null>(null);
  const [openAttempt, setOpenAttempt] = useState(0);
  const threadId = chatIdFromPath(path);
  const selected = ready && opened?.id === threadId ? opened : null;
  const [query,setQuery]=useState(""),[sidebar,setSidebar]=useState(false);
  const [turnSelections, setTurnSelections] = useState<Record<string, TurnSelection>>({});
  const [shortcuts, setShortcuts] = useState(readChatShortcuts);
  const [dialog,setDialog]=useState<Dialog|null>(null),[menu,setMenu]=useState(false),[error,setError]=useState(""),[notice,setNotice]=useState("");
  const [formName,setFormName]=useState(""),[formPath,setFormPath]=useState(""),[formProject,setFormProject]=useState(""),[formBusy,setFormBusy]=useState(false);
  const catalogState=useCatalog(ready,account,query,project,section==="archived");
  const {catalog}=catalogState;
  const projects=[...(catalog?.projects||[]),...(catalog?.otherProjects||[])];
  const selectedProject = projects.find(p => p.id === project);
  const newChatReady = !!catalog && (!project || !!selectedProject);
  const chatState=useChatHistory(ready,selected?.id||"",account);
  const history=chatState.history?.thread.id===selected?.id?chatState.history:undefined;
  const activity = useChatActivity(ready, selected?.id || "", account);
  const openActivity: ChatActivity | undefined = (selected && activity.threads?.[selected.id]) ||
    (history?.thread.externalOwner ? { source: "external", state: "unknown" } :
      history?.thread.status.type === "active" ? {
        source: "web", state: history.pending.length ? "waiting" : "running",
        turnId: history.turns.at(-1)?.id,
      } : undefined);
  const turns = useMemo(() => mergeLiveTurn(history?.turns ?? [], activity.turn), [history?.turns, activity.turn]);
  const liveTurnId = openActivity && openActivity.state !== "idle" && openActivity.state !== "unknown" ? openActivity.turnId : undefined;
  const projectNames = new Map<string, number>();
  for (const p of projects) projectNames.set(p.name, (projectNames.get(p.name) ?? 0) + 1);
  const scroll=useRef<HTMLDivElement>(null),stick=useRef(true),anchor=useRef<{height:number;top:number;thread:string;revision:number}|null>(null),olderBusy=useRef(false);
  const first=new Date().getHours()<12?"무엇부터 시작할까요?":"어떤 작업을 이어갈까요?";
  const accounts=catalog?.accounts||data.snapshot?.accounts||[];
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
  const modelState = useChatModels(ready && ((!!selected && !isArchived) || dialog?.kind === "settings"), account);
  const displayChats=section==="pinned"?(catalog?.pinned||[]).filter(c=>`${chatTitle(c)} ${c.cwd}`.toLowerCase().includes(query.toLowerCase())):catalog?.threads||[];

  useEffect(()=>{
    if (access.status === "anonymous") {
      setOpened(null);setOpenError(null);setDialog(null);setQuery("");setSidebar(false);setTurnSelections({});setAccount("default");
    }
  },[access.status]);
  useEffect(() => {
    const onPopState = () => {
      setPath(location.pathname);
      setOpened(null);
      setOpenError(null);
      setSidebar(false);
      setError("");
      setNotice("");
    };
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);
  useEffect(() => {
    if (!ready || !threadId || selected) return;
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
  }, [ready, threadId, selected, openAttempt]);
  useEffect(()=>{stick.current=true;anchor.current=null;olderBusy.current=false;setMenu(false);},[selected?.id,account]);
  useLayoutEffect(()=>{
    const node=scroll.current;if(!node)return;
    if(anchor.current&&anchor.current.thread===selected?.id&&anchor.current.revision===chatState.olderRevision){node.scrollTop=node.scrollHeight-anchor.current.height+anchor.current.top;anchor.current=null;}
    else if(stick.current&&!anchor.current)node.scrollTop=node.scrollHeight;
  },[turns,selected?.id,chatState.olderRevision]);
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
    setSidebar(false);setError("");setNotice("");
  }
  function filter(next:Section,id="") {setSection(next);setProject(id);}
  function openDialog(next:Dialog) {
    setError("");setMenu(false);setFormName("");
    const target=next.kind==="new"?selectedProject:undefined;
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
        const result=await mutate("web.create",{cwd:formPath,project_id:formProject});choose(result.thread);
      } else if(dialog.kind==="project") {
        const result=await mutate("web.projectCreate",{name:formName,cwd:formPath});filter("chats",result.project.id);setNotice("프로젝트를 추가했습니다.");
      } else if(dialog.kind==="move") {
        await mutate("web.move",{thread_id:dialog.chat.id,project_id:formProject});setNotice("프로젝트를 옮겼습니다.");void chatState.refresh();
      } else if(dialog.kind==="archive") {
        await mutate("web.archive",{thread_id:dialog.chat.id,archived:!dialog.chat.archived});navigate("",true);setOpened(null);setNotice(dialog.chat.archived?"보관을 해제했습니다.":"채팅을 보관했습니다.");
      } else if(dialog.kind==="delete") {
        await mutate("web.delete",{thread_id:dialog.chat.id});navigate("",true);setOpened(null);setNotice("채팅을 영구 삭제했습니다.");
      }
      setDialog(null);
    }catch(e){setError((e as Error).message);}finally{setFormBusy(false);}
  }
  async function pin() {
    if(!chat)return;setMenu(false);
    try {await mutate("web.pin",{thread_id:chat.id,pinned:!catalog?.pinned.some(t=>t.id===chat.id)});setNotice("고정 상태를 변경했습니다.");}
    catch(e){setError((e as Error).message);}
  }
  async function loadOlder() {
    const node=scroll.current;if(!node||!selected||olderBusy.current||!history?.nextCursor)return;
    olderBusy.current=true;anchor.current={height:node.scrollHeight,top:node.scrollTop,thread:selected.id,revision:chatState.olderRevision+1};
    const loaded=await chatState.loadOlder();if(!loaded)anchor.current=null;olderBusy.current=false;
  }

  if (access.status === "anonymous") return <Login passwordMode={access.password} message={access.message} />;
  if (!ready) return <main className="login">
    <p role={access.status === "unavailable" ? "alert" : "status"}>{access.message || "로그인 상태를 확인하고 있습니다…"}</p>
    {access.status === "unavailable" && <button type="button" onClick={() => void authenticate()}>다시 확인</button>}
  </main>;
  return <div className="chat-app">
    {sidebar&&<button className="sidebar-backdrop" aria-label="메뉴 닫기" onClick={()=>setSidebar(false)}/>}
    <aside className={`sidebar ${sidebar?"is-open":""}`} aria-label="탐색">
      <div className="brand"><span className="brand-icon">C</span><strong>CodexM</strong><span>이 머신</span><button className="mobile-close" aria-label="메뉴 닫기" onClick={()=>setSidebar(false)}>×</button></div>
      <button className="new-chat" aria-label="새 채팅" disabled={!newChatReady} onClick={()=>openDialog({kind:"new"})}><span aria-hidden="true">＋</span>새 채팅</button>
      <label className="sidebar-search"><span className="sr-only">채팅 검색</span><input placeholder="채팅 검색" value={query} onChange={e=>setQuery(e.target.value)}/></label>
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
      <div className="chat-list" aria-label="채팅 목록">{displayChats.map(t=><a key={t.id} href={chatPath(t.id)} aria-current={selected?.id===t.id ? "page" : undefined} title={chatTitle(t)} onClick={event => {
        if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        event.preventDefault();
        if (selected?.id !== t.id) choose(t);
        else setSidebar(false);
      }}>
        <strong>{chatTitle(t)}</strong>
        <span className="chat-row-meta"><small>{t.cwd.split("/").filter(Boolean).pop()||t.cwd}</small>
          {!t.archived && <ChatActivityLabel activity={activity.threads?.[t.id]} known={activity.writersKnown}/>}
        </span>
      </a>)}
        {!displayChats.length&&catalog&&!catalogState.loading&&<p className="muted empty-list">표시할 채팅이 없습니다.</p>}
        {catalogState.error&&<p role="alert" className="inline-error">{catalogState.error}</p>}
        {section!=="pinned"&&catalog?.nextCursor&&<button className="load-more" disabled={catalogState.loading} onClick={()=>void catalogState.refresh(catalog.nextCursor)}>채팅 더 보기</button>}
      </div>
      <div className="sidebar-footer"><button onClick={()=>openDialog({kind:"accounts"})}>계정 관리</button><button onClick={()=>{setSidebar(false);openDialog({kind:"settings"});}}>설정</button><button onClick={()=>void logout().catch(e=>setError(e.message))}>로그아웃</button></div>
    </aside>
    <main className="conversation" data-thread-id={selected?.id||""}>
      <header className="chat-header"><button className="menu-toggle" aria-label="메뉴 열기" onClick={()=>setSidebar(true)}>☰</button><div className="chat-heading"><h1>{chat?chatTitle(chat):"새로운 작업"}</h1><small>{chat?.cwd||"로컬 채팅 · 내 프로젝트"}</small></div>
        <div className={`chat-header-controls${chat && !isArchived ? " has-turn-settings" : ""}`}>
          <div className="account-header">
            <label className="account-select">계정
              <select aria-label="이번에 사용할 계정" value={account} disabled={!!threadId && !selected} onChange={e=>setAccount(e.target.value)}><option value="default">기본 계정</option>{accounts.map((a:any)=><option key={a.name} value={a.name}>{a.name}</option>)}</select>
            </label>
            {(!threadId || selected) && <AccountLimits key={account} account={account} />}
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
            deferred={!!(activity.accountTurn?.status === "inProgress" || history?.thread.status.type === "active" || history?.thread.externalOwner)}
          />}
        </div>
        {chat&&<div className="chat-menu"><button aria-label="채팅 메뉴" aria-expanded={menu} onClick={()=>setMenu(!menu)}>···</button>{menu&&<div className="popover" role="menu"><button role="menuitem" onClick={()=>void pin()}>{catalog?.pinned.some(t=>t.id===chat.id)?"고정 해제":"고정하기"}</button><button role="menuitem" onClick={()=>openDialog({kind:"move",chat:{...chat}})}>프로젝트로 이동</button><button role="menuitem" onClick={()=>openDialog({kind:"archive",chat:{...chat,archived:isArchived}})}>{isArchived?"보관 해제":"보관하기"}</button><button role="menuitem" className="danger" onClick={()=>openDialog({kind:"delete",chat:{...chat}})}>영구 삭제</button></div>}</div>}
      </header>
      {!dialog&&error&&<div role="alert" className="banner error-banner">{error}</div>}{notice&&<div role="status" className="banner notice-banner">{notice}<button aria-label="알림 닫기" onClick={()=>setNotice("")}>×</button></div>}
      <div className="message-scroll" ref={scroll} aria-label="대화 내용" onScroll={e=>{const n=e.currentTarget;stick.current=n.scrollHeight-n.clientHeight-n.scrollTop<100;if(n.scrollTop<90&&!chatState.loading)void loadOlder();}}>
        {selected?<div className="message-inner">
          {history?.nextCursor&&<button className="older-button" disabled={chatState.loadingOlder} onClick={()=>void loadOlder()}>{chatState.loadingOlder?"이전 메시지를 불러오는 중…":"이전 메시지 불러오기"}</button>}
          {chatState.loading&&<p className="loading-message" role="status">최근 대화를 불러오는 중…</p>}
          {chatState.error&&<p role="alert" className="inline-error">{chatState.error}</p>}
          {!!turns.length&&<ChatMessages thread={selected.id} turns={turns} account={account} external={history?.thread.externalOwner} liveTurnId={liveTurnId}/>}
          {history&&!turns.length&&<div className="empty-conversation"><h2>{isArchived?"보관된 대화입니다":"표시할 메시지가 없습니다"}</h2><p>{isArchived?"현재 Codex 원본에서 조회되는 메시지가 없습니다.":"요청을 입력하면 이 대화에서 작업을 이어갑니다."}</p></div>}
          {history?.pending.map(p=><Approval key={p.request_id} request={p} thread={selected.id} account={account} onDone={()=>void chatState.refresh()}/>)}
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
      {chat&&!isArchived&&<Composer
        key={composerKey}
        chat={chat}
        account={account}
        history={history}
        liveTurn={activity.accountTurn}
        models={modelState.models}
        selection={selection}
        onSent={()=>{stick.current=true;void chatState.refresh();void catalogState.refresh();}}
      />}
      {selected&&isArchived&&<div className="archived-note">보관된 채팅입니다.<button onClick={()=>openDialog({kind:"archive",chat:{...chat!,archived:true}})}>보관 해제</button></div>}
    </main>
    {dialog&&<Modal title={dialog.kind==="settings"?"단축키 설정":dialog.kind==="new"?"새 채팅":dialog.kind==="project"?"프로젝트 추가":dialog.kind==="move"?"프로젝트로 이동":dialog.kind==="archive"?(dialog.chat.archived?"보관 해제":"채팅 보관"):dialog.kind==="delete"?"채팅 영구 삭제":"계정 관리"} onClose={()=>{if(!formBusy)setDialog(null);}}>
      {dialog.kind==="accounts"?<Accounts accounts={accounts}/>:dialog.kind==="settings"?<ShortcutSettings
        bindings={shortcuts}
        models={modelState.models}
        modelsLoading={modelState.loading}
        modelError={modelState.error}
        retryModels={modelState.retry}
        onSave={bindings=>{setShortcuts(bindings);setDialog(null);}}
        onClose={()=>setDialog(null)}
      />:<form className="dialog-form" onSubmit={e=>void submitDialog(e)}>
        {"chat"in dialog&&<p className="dialog-chat-title">{chatTitle(dialog.chat)}</p>}
        {(dialog.kind==="new"||dialog.kind==="move")&&<label>프로젝트<select aria-label="프로젝트" value={formProject} onChange={e=>{setFormProject(e.target.value);const chosen=projects.find(p=>p.id===e.target.value);if(chosen?.roots[0])setFormPath(chosen.roots[0].path);}}><option value="">프로젝트 없음</option>{projects.map(p=><option key={p.id} value={p.id}>{p.name}</option>)}</select></label>}
        {dialog.kind==="project"&&<label>프로젝트 이름<input value={formName} onChange={e=>setFormName(e.target.value)} placeholder="폴더 이름 사용" maxLength={200}/></label>}
        {(dialog.kind==="new"||dialog.kind==="project")&&<label>이 머신의 작업 폴더<input value={formPath} onChange={e=>setFormPath(e.target.value)} placeholder="/absolute/project/path" required spellCheck={false}/></label>}
        {dialog.kind==="move"&&<p className="muted">채팅의 프로젝트 소속을 변경합니다. 작업 폴더와 파일을 옮기지는 않습니다.</p>}
        {dialog.kind==="archive"&&<p>{dialog.chat.archived?"채팅을 일반 목록으로 복원합니다.":"진행 중인 작업과 하위 채팅이 중단될 수 있습니다. 채팅을 보관할까요?"}</p>}
        {dialog.kind==="delete"&&<p className="danger">이 채팅과 하위 대화를 영구 삭제합니다. 되돌릴 수 없습니다.</p>}
        {error&&<p role="alert" className="inline-error">{error}</p>}
        <div className="button-row"><button type="button" onClick={()=>setDialog(null)} disabled={formBusy}>취소</button><button className={dialog.kind==="delete"?"danger-button":"primary"} type="submit" disabled={formBusy}>{formBusy?"처리 중…":dialog.kind==="delete"?"영구 삭제":dialog.kind==="new"?"채팅 만들기":dialog.kind==="project"?"프로젝트 추가":"적용"}</button></div>
      </form>}
    </Modal>}
  </div>;
}

function Login({passwordMode,message}:{passwordMode:boolean;message:string}) {
  const [username,setUsername]=useState(""),[password,setPassword]=useState(""),[token,setToken]=useState(""),[busy,setBusy]=useState(false),[error,setError]=useState("");
  return <main className="login"><div className="brand-icon">C</div><p className="eyebrow">CODEXM</p><h1>내 작업에 연결하세요</h1><p>이 머신의 채팅과 프로젝트를<br/>브라우저에서 이어갑니다.</p><form onSubmit={e=>{e.preventDefault();if(busy)return;setBusy(true);setError("");void(passwordMode?login(username,password):exchange(token)).catch(e=>setError(e.message)).finally(()=>setBusy(false));}}>
    {passwordMode?<><label>아이디<input autoComplete="username" value={username} onChange={e=>setUsername(e.target.value)} required/></label><label>비밀번호<input type="password" autoComplete="current-password" value={password} onChange={e=>setPassword(e.target.value)} required/></label></>:<label>로컬 접속 코드<input type="password" value={token} onChange={e=>setToken(e.target.value)} required/></label>}
    <button className="primary" disabled={busy}>{busy?"연결 중…":passwordMode?"로그인":"연결"}</button>
  </form>{(error||message)&&<p role="alert">{error||message}</p>}</main>;
}
