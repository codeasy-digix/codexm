import { useCallback, useEffect, useRef, useState } from "react";
import { call, connectionAvailable, method } from "../../client/data";
import { mergeTurns, workingActivity, type ChatActivitySnapshot, type ChatHistory, type ChatHistoryPage, type ChatHistoryUpdate, type Catalog, type ChatModel, type ChatThread } from "../contracts/web";
import { RecentHistory, transcriptBasis, updateTranscript, type Transcript } from "./recent-history";

export function useChatActivity(enabled: boolean, thread: string, account: string) {
  const [observation, setObservation] = useState<{ thread: string; account: string; snapshot: ChatActivitySnapshot }>();
  useEffect(() => {
    if (!enabled) {
      setObservation(undefined);
      return;
    }
    let active = true;
    let sampling = false;
    let last = "";
    let timer: ReturnType<typeof setTimeout>;
    const sample = async () => {
      if (!active || sampling || document.visibilityState === "hidden") return;
      clearTimeout(timer);
      if (!connectionAvailable()) {
        timer = setTimeout(sample, 1500);
        return;
      }
      sampling = true;
      let delay = 1500;
      try {
        const snapshot = await call<ChatActivitySnapshot>("web.activity", { thread_id: thread, account });
        if (!active) return;
        const fingerprint = JSON.stringify(snapshot);
        if (fingerprint !== last) {
          last = fingerprint;
          setObservation({ thread, account, snapshot });
        }
        const selected = snapshot.threads[thread];
        if (workingActivity(selected) || selected?.state === "waiting") delay = selected?.source === "web" ? 350 : 1000;
      } catch {
        last = "";
        if (active && connectionAvailable()) setObservation(undefined);
      } finally {
        sampling = false;
        if (active) timer = setTimeout(sample, delay);
      }
    };
    document.addEventListener("visibilitychange", sample);
    void sample();
    return () => {
      active = false;
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", sample);
    };
  }, [enabled, thread, account]);
  const selected = enabled && observation?.thread === thread && observation.account === account ? observation.snapshot : undefined;
  return {
    threads: enabled ? observation?.snapshot.threads : undefined,
    writersKnown: enabled && !!observation?.snapshot.writersKnown,
    turn: selected?.turn,
    accountTurn: selected?.accountTurn ? selected.turn : undefined,
    pending: selected?.pending,
    prediction: selected?.prediction,
  };
}

export function useChatModels(enabled: boolean, account: string, generation?: number) {
  const [result, setResult] = useState<{ account: string; generation?: number; models: ChatModel[]; error?: string }>();
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!enabled) return;
    let active = true;
    setResult(undefined);
    void call<ChatModel[]>("web.models", { account }).then(models => {
      if (active) setResult({ account, generation, models });
    }).catch(error => {
      if (active) setResult({ account, generation, models: [], error: (error as Error).message });
    });
    return () => { active = false; };
  }, [enabled, account, generation, attempt]);
  const current = result?.account === account && result.generation === generation ? result : undefined;
  return {
    models: current?.models ?? [],
    error: current?.error,
    loading: enabled && !current,
    retry: () => setAttempt(previous => previous + 1),
  };
}

export function useCatalog(ready:boolean,search:string,project:string,archived:boolean,openThread:string,online:boolean) {
  const [catalog,setCatalog]=useState<Catalog>(),[error,setError]=useState(""),[loading,setLoading]=useState(false);
  const generation=useRef(0),busy=useRef(false),visibleCount=useRef(50);
  const refresh=useCallback(async(cursor?:string|null,reset=false)=>{
    const epoch=generation.current;
    if (!ready||!connectionAvailable()||busy.current) return;
    busy.current=true;setLoading(true);
    try {const params={search,project_id:project,archived};
      let page=await call<Catalog>("web.catalog",{...params,...(cursor?{cursor}:{})});
      // Refresh every page already on screen. Retaining an old tail made
      // GUI-archived/deleted/moved chats linger until the browser was reloaded.
      if(!cursor){const seen=new Set<string>();let rounds=0;
        while(page.nextCursor&&page.threads.length<visibleCount.current&&rounds++<200){
          if(seen.has(page.nextCursor))throw new Error("목록 페이지가 진행되지 않습니다. 새로고침해 주세요.");seen.add(page.nextCursor);
          const more=await call<Catalog>("web.catalog",{...params,cursor:page.nextCursor});
          if(epoch!==generation.current)return;
          const ids=new Set(page.threads.map(t=>t.id));page={...page,threads:[...page.threads,...more.threads.filter(t=>!ids.has(t.id))],nextCursor:more.nextCursor};
        }
      }
      if(epoch!==generation.current)return;
      setCatalog(old=>{
        if(cursor&&old){const threads=[...old.threads,...(page.threads||[]).filter(t=>!old.threads.some(x=>x.id===t.id))];visibleCount.current=threads.length;return {...old,threads,nextCursor:page.nextCursor};}
        const next={...page,threads:page.threads||[],projects:page.projects||[],pinned:page.pinned||[]};
        return next;
      });setError("");
    }catch(e){if(epoch===generation.current&&connectionAvailable())setError((e as Error).message);}finally{if(epoch===generation.current){busy.current=false;setLoading(false);}}
  },[ready,search,project,archived]);
  const refreshList = useRef(refresh);
  refreshList.current = refresh;
  useEffect(() => {
    if (!online) return;
    let cancelled = false;
    void method("codexm.refreshCatalog", []).then(() => {
      if (!cancelled) void refreshList.current();
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [online, openThread]);
  useEffect(()=>{
    generation.current++;busy.current=false;visibleCount.current=50;
    setCatalog(previous=>ready&&previous?{...previous,threads:[],nextCursor:undefined}:undefined);
    setLoading(ready);setError("");
    if(!ready)return;
    const initial=setTimeout(()=>void refresh(),search?250:0);
    const timer=setInterval(()=>{if(document.visibilityState!=="hidden")void refresh();},7000);
    return()=>{clearTimeout(initial);clearInterval(timer);generation.current++;busy.current=false;};
  },[refresh,ready]);
  return {catalog,error,loading,refresh};
}

function cachedPage(thread: string, cursor?: string) {
  return method<ChatHistoryPage | null>("codexm.cachedHistory", [{ thread_id: thread, cursor, ...(!cursor && { history_basis: { turns: [] } }) }]).catch(() => null);
}

type HistoryView = { key: string; page?: Transcript; metadata: ChatHistoryUpdate; checkingCache: boolean; refreshing: boolean; older: boolean };

export function useChatHistory(ready: boolean, chat: ChatThread | null, account: string, online: boolean) {
  const thread = chat?.id ?? "";
  const key = ready && thread ? `${thread}:${account}` : "";
  const [memory] = useState(() => new RecentHistory());
  const [current, setCurrent] = useState<{ key: string; history: ChatHistory }>();
  const [error, setError] = useState("");
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [olderRevision, setOlderRevision] = useState(0);
  const view = useRef<HistoryView>(null);
  const selected = useRef(chat);
  selected.current = chat;

  const publish = useCallback((run: HistoryView, id: string, page: Transcript) => {
    run.page = page;
    memory.retain(id, page);
    setCurrent({ key: run.key, history: { ...run.metadata, ...page } });
  }, [memory]);

  const refresh = useCallback(async () => {
    const run = view.current;
    if (!run || run.key !== key || run.refreshing || run.checkingCache || !connectionAvailable()) return;
    run.refreshing = true;
    try {
      const read = (page?: Transcript) => call<ChatHistoryUpdate>("web.history", {
        thread_id: thread, account, history_basis: transcriptBasis(page),
      });
      let update = await read(run.page);
      if (view.current !== run) return;
      let page = updateTranscript(run.page, update);
      if (!page) {
        update = await read();
        if (view.current !== run) return;
        page = updateTranscript(run.page, update);
      }
      if (!page) throw new Error("변경된 채팅 이력을 불러오지 못했습니다.");
      run.metadata = { ...update, queue: update.queue ?? [], pending: update.pending ?? [] };
      publish(run, thread, page);
      setError("");
    } catch (reason) {
      if (view.current === run && connectionAvailable()) setError((reason as Error).message);
    } finally {
      run.refreshing = false;
    }
  }, [key, thread, account, publish]);

  useEffect(() => {
    setCurrent(undefined);
    setError("");
    setLoadingOlder(false);
    setOlderRevision(0);
    if (!ready) memory.clear();
    if (!key || !selected.current) return;
    const run: HistoryView = {
      key, page: memory.peek(thread), metadata: { thread: selected.current, queue: [], pending: [] },
      checkingCache: false, refreshing: false, older: false,
    };
    view.current = run;
    if (run.page) {
      publish(run, thread, run.page);
      void refresh();
    } else if (connectionAvailable()) {
      run.checkingCache = true;
      void cachedPage(thread).then(page => {
        if (view.current !== run) return;
        run.checkingCache = false;
        if (page && !run.page) publish(run, thread, page);
        void refresh();
      });
    }
    const visible = () => { if (document.visibilityState === "visible") void refresh(); };
    const timer = setInterval(visible, 2500);
    document.addEventListener("visibilitychange", visible);
    return () => {
      view.current = null;
      clearInterval(timer);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [ready, key, thread, memory, publish, refresh]);
  useEffect(() => { if (online) void refresh(); }, [online, refresh]);

  const loadOlder = useCallback(async () => {
    const run = view.current;
    const cursor = run?.page?.nextCursor;
    if (!run || run.key !== key || !cursor || run.older || !connectionAvailable()) return false;
    run.older = true;
    setLoadingOlder(true);
    try {
      const page = await cachedPage(thread, cursor) ?? await call<ChatHistoryPage>("web.history", { thread_id: thread, account, cursor });
      if (view.current !== run || run.page?.nextCursor !== cursor) return false;
      publish(run, thread, { ...run.page, turns: mergeTurns(run.page.turns, page.turns ?? [], true), nextCursor: page.nextCursor });
      setOlderRevision(n => n + 1);
      return true;
    } catch (reason) {
      if (view.current === run && connectionAvailable()) setError((reason as Error).message);
      return false;
    } finally {
      run.older = false;
      if (view.current === run) setLoadingOlder(false);
    }
  }, [key, thread, account, publish]);

  function forget(id: string) {
    memory.forget(id);
    if (view.current?.metadata.thread.id === id) view.current = null;
  }
  const warm = key ? memory.peek(thread) : undefined;
  const history = key && current?.key === key ? current.history : warm && chat ? { ...warm, thread: chat, queue: [], pending: [] } : undefined;
  return { history, error, loading: !!key && !history && !error, loadingOlder, olderRevision, refresh, loadOlder, forget };
}
