import React, { useState } from "react";
import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { call } from "../../client/data";
import type { ChatItem, ChatTurn, PendingRequest } from "../contracts/web";

const markdownComponents: Components = {
  table({ node: _node, ...props }) {
    return <div className="markdown-table-scroll" role="region" aria-label="채팅 표" tabIndex={0}>
      <table {...props} />
    </div>;
  },
  th({ node: _node, ...props }) {
    return <th scope="col" {...props} />;
  },
};

export function inputText(content?: Array<{type: string; text?: string}>) {
  return (content || []).map(part => part.text || (part.type === "image" || part.type === "localImage" ? "[이미지 첨부]" : "")).filter(Boolean).join("\n");
}

function detailTitle(item: ChatItem) {
  if (item.type === "commandExecution") return String(item.command || "명령 실행").split("\n")[0].slice(0,100);
  if (item.type === "mcpToolCall") return `${item.server || "도구"} · ${item.tool || "호출"}`;
  if (item.type === "fileChange") return "파일 변경";
  if (item.type === "reasoning") return "검토 과정";
  if (item.type === "agentMessage") return "진행 안내";
  return item.type;
}

type ItemEntry = { item: ChatItem; outputDeferred?: boolean };
function ToolItem({entry,thread,turn,account}:{entry:ItemEntry;thread:string;turn:string;account:string}) {
  const [full,setFull]=useState<ChatItem>(),[loading,setLoading]=useState(false),[error,setError]=useState("");
  const item=full||entry.item;
  async function expand(open:boolean) {
    if (!open || !entry.outputDeferred || full || loading) return;
    setLoading(true);setError("");
    try {const reply=await call<{data:ItemEntry[]}>("web.item",{thread_id:thread,turn_id:turn,item_id:item.id,account});setFull(reply.data[0]?.item);}
    catch(e){setError((e as Error).message);}finally{setLoading(false);}
  }
  const output = item.type === "agentMessage" ? item.text : item.aggregatedOutput;
  return <details className="tool-item" onToggle={e=>void expand(e.currentTarget.open)}>
    <summary><span>{detailTitle(item)}</span><small>{String(item.status||"")}</small></summary>
    {loading?<p className="muted" role="status">출력을 불러오는 중…</p>:error?<p role="alert">{error}</p>:typeof output==="string"?<pre>{output}</pre>:<pre>{JSON.stringify(item,null,2)}</pre>}
  </details>;
}

function TurnDetails({thread,turn,account,external,current}:{thread:string;turn:ChatTurn;account:string;external?:boolean;current:boolean}) {
  const [entries,setEntries]=useState<ItemEntry[]>([]),[cursor,setCursor]=useState<string|null>(),[started,setStarted]=useState(false),[loading,setLoading]=useState(false),[error,setError]=useState("");
  async function load(next?:string|null) {
    if (loading) return;setLoading(true);setError("");
    try {const page=await call<{data:ItemEntry[];nextCursor?:string|null}>("web.items",{thread_id:thread,turn_id:turn.id,account,...(next?{cursor:next}:{})});setEntries(old=>next?[...old,...page.data]:page.data);setCursor(page.nextCursor);setStarted(true);}
    catch(e){setError((e as Error).message);}finally{setLoading(false);}
  }
  const details=entries.filter(({item})=>item.type!=="userMessage"&&(item.type!=="agentMessage"||item.phase==="commentary"));
  return <details className="turn-details" onToggle={e=>{if(e.currentTarget.open&&!started&&!loading)void load();}}>
    <summary>작업 과정 <span>{current||turn.status==="inProgress"?"진행 중":turn.status==="interrupted"?(external?"원래 창에서 상태 확인":"중단됨"):"보기"}</span></summary>
    {error&&<p role="alert">{error}</p>}
    {details.map((entry,i)=><ToolItem key={`${entry.item.id}-${i}`} entry={entry} thread={thread} turn={turn.id} account={account}/>)}
    {started&&!details.length&&!loading&&<p className="muted">별도 작업 과정이 없습니다.</p>}
    {loading&&<p role="status">작업 과정을 불러오는 중…</p>}
    {cursor&&<button className="text-button" onClick={()=>void load(cursor)} disabled={loading}>과정 더 보기</button>}
  </details>;
}

export function ChatMessages({thread,turns,account,external,liveTurnId}:{thread:string;turns:ChatTurn[];account:string;external?:boolean;liveTurnId?:string}) {
  return <div className="turns">{turns.map(turn=><article className="turn" key={turn.id} data-turn-id={turn.id}>
    {turn.items.filter(item=>item.type==="userMessage").map(item=><div className="user-message" key={item.id}><span className="sr-only">나</span><p>{inputText(item.content)}</p></div>)}
    <div className="assistant-message">
      {turn.items.filter(item => item.type === "agentMessage" && (item.phase !== "commentary" || turn.id === liveTurnId) && item.text).map(item =>
        item.streaming || item.phase === "commentary"
          ? <div className={`live-message${item.phase === "commentary" ? " commentary-message" : ""}`} key={item.id}>{item.text}</div>
          : <div className="markdown" key={item.id}>
            <Markdown remarkPlugins={[remarkGfm]} components={markdownComponents}>{item.text || ""}</Markdown>
          </div>)}
      {turn.error&&<p role="alert" className="inline-error">{turn.error.message}</p>}
      <TurnDetails thread={thread} turn={turn} account={account} external={external} current={turn.id === liveTurnId}/>
    </div>
  </article>)}</div>;
}

export function Approval({request,thread,account,onDone}:{request:PendingRequest;thread:string;account:string;onDone:()=>void}) {
  const [busy,setBusy]=useState(false),[error,setError]=useState(""),[answers,setAnswers]=useState<Record<string,string>>({});
  const input=request.method==="item/tool/requestUserInput";
  const questions:Array<{id:string;question:string;header?:string;options?:Array<{label:string;description?:string}>}>=request.params.questions||[];
  async function respond(accept:boolean) {
    setBusy(true);setError("");
    const response=input?{answers:Object.fromEntries(questions.map(q=>[q.id,{answers:[answers[q.id]||""]}]))}:{decision:accept?"accept":"decline"};
    try {await call("web.respond",{thread_id:thread,account,request_id:request.request_id,response},crypto.randomUUID());onDone();}
    catch(e){setError((e as Error).message);}finally{setBusy(false);}
  }
  return <section className="approval" aria-label={input?"추가 확인":"실행 승인"}>
    <h3>{input?"추가 확인이 필요합니다":request.method.includes("fileChange")?"파일 변경 승인":"명령 실행 승인"}</h3>
    {input?questions.map(q=><label key={q.id}>{q.question}{q.options?.length?<select value={answers[q.id]||""} onChange={e=>setAnswers({...answers,[q.id]:e.target.value})}><option value="">선택하세요</option>{q.options.map(o=><option key={o.label} value={o.label}>{o.label}</option>)}</select>:<input value={answers[q.id]||""} onChange={e=>setAnswers({...answers,[q.id]:e.target.value})}/>}</label>):<pre>{typeof request.params.command==="string"?request.params.command:JSON.stringify(request.params.command||request.params.reason||request.params,null,2)}</pre>}
    {error&&<p role="alert">{error}</p>}
    <div className="button-row">{!input&&<button onClick={()=>void respond(false)} disabled={busy}>거절</button>}<button className="primary" onClick={()=>void respond(true)} disabled={busy||(input&&questions.some(q=>!answers[q.id]))}>{input?"답변 전달":"이번 실행 승인"}</button></div>
  </section>;
}
