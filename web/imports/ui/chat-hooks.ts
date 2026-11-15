import { useCallback, useEffect, useRef, useState } from "react";
import { call } from "../../client/data";
import { mergeTurns, type ChatHistory, type Catalog } from "../contracts/web";

export function useCatalog(ready:boolean,account:string,search:string,project:string,archived:boolean) {
  const [catalog,setCatalog]=useState<Catalog>(),[error,setError]=useState(""),[loading,setLoading]=useState(false);
  const generation=useRef(0),busy=useRef(false);
  const refresh=useCallback(async(cursor?:string|null,reset=false)=>{
    const epoch=generation.current;
    if (!ready||busy.current) return;
    busy.current=true;setLoading(true);
    try {const page=await call<Catalog>("web.catalog",{account,search,project_id:project,archived,...(cursor?{cursor}:{})});
      if(epoch!==generation.current)return;
      setCatalog(old=>{
        if(cursor&&old)return {...old,threads:[...old.threads,...(page.threads||[]).filter(t=>!old.threads.some(x=>x.id===t.id))],nextCursor:page.nextCursor};
        const next={...page,threads:page.threads||[],projects:page.projects||[],pinned:page.pinned||[]};
        if(!reset&&old&&old.threads.length>next.threads.length){const ids=new Set(next.threads.map(t=>t.id));return {...next,threads:[...next.threads,...old.threads.filter(t=>!ids.has(t.id))],nextCursor:old.nextCursor};}
        return next;
      });setError("");
    }catch(e){if(epoch===generation.current)setError((e as Error).message);}finally{if(epoch===generation.current){busy.current=false;setLoading(false);}}
  },[ready,account,search,project,archived]);
  useEffect(()=>{
    generation.current++;busy.current=false;setCatalog(undefined);setError("");
    if(!ready)return;
    const initial=setTimeout(()=>void refresh(),search?250:0);
    const timer=setInterval(()=>{if(document.visibilityState!=="hidden")void refresh();},7000);
    return()=>{clearTimeout(initial);clearInterval(timer);generation.current++;busy.current=false;};
  },[refresh,ready]);
  return {catalog,error,loading,refresh};
}

export function useChatHistory(ready:boolean,thread:string,account:string) {
  const [history,setHistory]=useState<ChatHistory>(),[error,setError]=useState(""),[loading,setLoading]=useState(false),[loadingOlder,setLoadingOlder]=useState(false),[olderRevision,setOlderRevision]=useState(0);
  const generation=useRef(0),polling=useRef(false),older=useRef(false);
  const refresh=useCallback(async()=>{
    if(!ready||!thread||polling.current)return;
    const epoch=generation.current;polling.current=true;
    try {const page=await call<ChatHistory>("web.history",{thread_id:thread,account});if(epoch!==generation.current)return;
      page.turns=page.turns||[];page.queue=page.queue||[];page.pending=page.pending||[];
      setHistory(old=>{
        if(!old||!old.turns.length)return page;
        // A hidden tab may miss many turns. Reset to a correctly paginated
        // latest window rather than silently leaving a hole in the transcript.
        if(page.turns.length&&!page.turns.some(t=>old.turns.some(x=>x.id===t.id)))return page;
        return {...page,turns:mergeTurns(old.turns,page.turns),nextCursor:old.nextCursor};
      });setError("");
    }catch(e){if(epoch===generation.current)setError((e as Error).message);}finally{if(epoch===generation.current){polling.current=false;setLoading(false);}}
  },[ready,thread,account]);
  useEffect(()=>{
    generation.current++;polling.current=false;older.current=false;setHistory(undefined);setError("");setLoading(!!thread);setLoadingOlder(false);setOlderRevision(0);
    if(!ready||!thread)return;
    void refresh();
    const timer=setInterval(()=>{if(document.visibilityState!=="hidden")void refresh();},1500);
    const visible=()=>{if(document.visibilityState==="visible")void refresh();};document.addEventListener("visibilitychange",visible);
    return()=>{generation.current++;polling.current=false;clearInterval(timer);document.removeEventListener("visibilitychange",visible);};
  },[ready,thread,refresh]);
  const loadOlder=useCallback(async()=>{
    if(!history?.nextCursor||older.current)return false;
    const epoch=generation.current;older.current=true;setLoadingOlder(true);
    try {const page=await call<ChatHistory>("web.history",{thread_id:thread,account,cursor:history.nextCursor});if(epoch!==generation.current)return false;
      setHistory(current=>current?{...current,turns:mergeTurns(current.turns,page.turns||[],true),nextCursor:page.nextCursor}:page);setOlderRevision(v=>v+1);return true;
    }catch(e){if(epoch===generation.current)setError((e as Error).message);return false;}finally{if(epoch===generation.current){older.current=false;setLoadingOlder(false);}}
  },[history?.nextCursor,thread,account]);
  return {history,error,loading,loadingOlder,olderRevision,refresh,loadOlder};
}
