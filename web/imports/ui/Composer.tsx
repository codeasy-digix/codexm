import React, { useRef, useState } from "react";
import { call } from "../../client/data";
import { composerAction, type ChatHistory } from "../contracts/web";

export function Composer({thread,account,history,onSent}:{thread:string;account:string;history?:ChatHistory;onSent:()=>void}) {
  const [text,setText]=useState(""),[busy,setBusy]=useState(false),[error,setError]=useState(""),[notice,setNotice]=useState(""),[uncertain,setUncertain]=useState(false);
  const inFlight=useRef(false);
  const active=history?.thread.status.type==="active",external=history?.thread.externalOwner;
  const turn=active?history?.turns.slice().reverse().find(t=>t.status==="inProgress")?.id:undefined;
  const queue=history?.queue||[];
  async function send(mode:"immediate"|"queue") {
    if(!text.trim()||inFlight.current||uncertain)return;
    if(mode==="immediate"&&external){setError("이 채팅은 다른 GUI/CLI가 소유하고 있습니다. 원래 창에서 해제하거나 큐에 추가하세요.");return;}
    inFlight.current=true;setBusy(true);setError("");setNotice("");const sent=text;
    try {await call("web.send",{thread_id:thread,account,text:sent,mode,...(mode==="immediate"&&turn?{turn_id:turn}:{})},crypto.randomUUID());setText(current=>current===sent?"":current);setNotice(mode==="queue"?"큐에 추가했습니다.":active?"현재 작업에 전달했습니다.":"작업을 시작했습니다.");onSent();}
    catch(e){const message=(e as Error).message;setError(message);if(/outcome|확인하지 못|대기 시간|연결이 끊|unknown|timed out|deadline|EOF|disconnected/i.test(message))setUncertain(true);}
    finally{inFlight.current=false;setBusy(false);}
  }
  async function stop() {
    if(!turn||inFlight.current)return;inFlight.current=true;setBusy(true);setError("");
    try {await call("web.interrupt",{thread_id:thread,account,turn_id:turn},crypto.randomUUID());onSent();}
    catch(e){setError((e as Error).message);}finally{inFlight.current=false;setBusy(false);}
  }
  return <div className="composer-wrap">
    {queue.length>0&&<details className="queue-preview"><summary>대기 중인 입력 {queue.length}개</summary>{queue.map(q=><p key={q.id}>{q.input.map(x=>x.text||"").join("\n")}</p>)}</details>}
    {external&&<p className="owner-note">다른 GUI/CLI에서 사용 중 · 큐 입력은 원래 실행 계정에서 처리됩니다.</p>}
    {error&&<p role="alert" className="inline-error">{error}</p>}{uncertain&&<p className="inline-error">전송 결과가 불확실하여 재전송을 막았습니다. 대화와 큐에서 원래 입력을 확인하세요.</p>}
    <div className="composer">
      <textarea aria-label="메시지" value={text} onChange={e=>setText(e.target.value)} placeholder={active?"현재 작업에 개입하거나 다음 요청을 큐에 추가하세요":"Codex에게 요청하세요"} rows={3} enterKeyHint="send" onKeyDown={e=>{const mode=composerAction({...e,isComposing:e.nativeEvent.isComposing,keyCode:e.keyCode});if(mode){e.preventDefault();void send(mode);}}}/>
      <div className="composer-bottom"><small>Enter 개입 · Shift+Enter 큐 · Alt+Enter 줄바꿈</small><div className="composer-actions">
        {active&&!external&&<button className="stop-button" onClick={()=>void stop()} disabled={busy} aria-label="현재 턴 중지">■</button>}
        <button onClick={()=>void send("queue")} disabled={busy||uncertain||!text.trim()}>큐에 추가</button>
        <button className="primary" onClick={()=>void send("immediate")} disabled={busy||uncertain||!text.trim()||external}>{busy?"전달 중…":active?"즉시 개입":"보내기"}</button>
      </div></div>
    </div>
    {notice&&<p role="status" className="delivery-notice">{notice}</p>}
  </div>;
}
