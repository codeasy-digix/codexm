export function remaining(window) {
  return window && Number.isFinite(window.usedPercent ?? window.used_percent)
    ? Math.max(0, Math.min(100, 100 - (window.usedPercent ?? window.used_percent))) : null;
}
export function quota(snapshot) {
  const bucket = snapshot?.rateLimitsByLimitId?.codex ?? snapshot?.rateLimits ?? {};
  return {primary: remaining(bucket.primary), secondary: remaining(bucket.secondary), bucket};
}
export function label(state) {
  return ({running:'실행 중', idle:'대기', paused:'일시중지', stopped:'중지', complete:'완료', queued:'실행 대기', starting:'시작 중', switching:'계정 전환 중', quota_wait:'한도 대기', budget_limited:'목표 예산 도달', blocked:'진행 막힘', error:'오류', saved:'저장된 대화', observed:'열람', archived:'보관', result_ready:'결과 확인', waiting_on_user:'답변 필요', waiting_on_approval:'승인 필요', external_writer:'다른 Codex에서 실행', auth_required:'로그인 필요'})[state] ?? state ?? '미확인';
}
export function date(value) {const d=new Date(value);return value && !Number.isNaN(+d) ? d.toLocaleString('ko-KR',{month:'numeric',day:'numeric',hour:'2-digit',minute:'2-digit'}) : '미관측';}
export function duration(start,end=Date.now()) {const n=Math.max(0,Math.floor((+new Date(end)-+new Date(start))/1000));return Number.isFinite(n) ? n>=3600?`${Math.floor(n/3600)}시간 ${Math.floor(n%3600/60)}분`:n>=60?`${Math.floor(n/60)}분 ${n%60}초`:`${n}초`:'—';}
export function entryKey(e) {return e?.job_id || e?.thread_id || e?.id || '';}
export function filterSessions(entries,search,filter,project,stars=[]) {
  const q=search.trim().toLocaleLowerCase();
  return entries.filter(e=>(!q || `${e.title} ${e.cwd} ${e.account||''} ${e.thread_id||''}`.toLocaleLowerCase().includes(q)) && (!project || e.cwd===project) && (filter==='all' || filter==='managed'&&e.managed || filter==='running'&&['running','starting','switching','queued'].includes(e.state) || filter==='attention'&&['error','blocked','waiting_on_user','waiting_on_approval','auth_required','budget_limited','quota_wait','external_writer'].includes(e.state) || filter==='starred'&&stars.includes(entryKey(e))));
}

export function mergeTurns(older, latest) {
  const known=new Set(latest.map(t=>t.id).filter(Boolean));
  return [...older.filter(t=>!known.has(t.id)),...latest];
}

export function accountNames(snapshot) {
  const configured=snapshot?.priority||[],saved=snapshot?.account_names;
  // The registry is authoritative; historical quota observations cannot restore
  // a removed alias. Older servers without a registry retain their fallback.
  const names=Array.isArray(saved)?[...configured.filter(name=>saved.includes(name)),...saved]:[...configured,...(snapshot?.status?.accounts||[]).map(a=>a.account)];
  return [...new Set(names.filter(name=>typeof name==='string'&&/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(name)))];
}
export function loginPending(login) {return ['pending','verifying'].includes(login?.status);}
export function safeLoginURL(value) {
  try{const url=new URL(value);return ['https://auth.openai.com','https://auth0.openai.com','https://chatgpt.com'].includes(url.origin)&&!url.username&&!url.password?url.href:null;}catch{return null;}
}

const UI_BUILD='__CODEXM_UI_BUILD__';
// Inline panels have an intrinsic work area; fullscreen and fixed host frames
// allocate the viewport instead. The iframe's current height is never an input
// to the inline content height, so a host autosize update cannot feed itself.
export function surfaceLayout(context={}) {
  const dimensions=context.containerDimensions||{},positive=value=>Number.isFinite(value)&&value>0?value:null;
  const height=positive(dimensions.height),maxHeight=positive(dimensions.maxHeight),displayMode=context.displayMode||'inline';
  return {kind:height!=null?'fixed':['fullscreen','pip'].includes(displayMode)?'viewport':'content',displayMode,height,maxHeight};
}
export function panelSize(rect,width) {
  if(!Number.isFinite(rect?.height)||rect.height<=0||!Number.isFinite(width)||width<=0)return null;
  return {height:Math.ceil(rect.height),width:Math.ceil(width)};
}
export function deepLinkView(context) {
  const path=context?.['openai/deepLink']?.url;
  if(typeof path!=='string'||!path.startsWith('/')||path.startsWith('//'))return null;
  try{const url=new URL(path,'https://codexm.invalid');if(url.origin!=='https://codexm.invalid'||url.hash)return null;return url.pathname==='/'?'tasks':url.pathname==='/accounts'?'accounts':null;}catch{return null;}
}
export class Bridge {
  constructor(){this.pending=new Map();this.id=0;this.compat=false;this.hostContext={};this.sizeFrame=null;this.lastSize='';window.addEventListener('message',event=>this.receive(event));}
  receive(event){
    if(event.source!==window.parent || event.data?.jsonrpc!=='2.0')return;
    const m=event.data, p=this.pending.get(m.id);
    if(p){clearTimeout(p.timer);this.pending.delete(m.id);m.error?p.reject(new Error(m.error.message||'호스트 요청 오류')):p.resolve(m.result);return;}
    if(m.method==='ping'&&m.id!=null){window.parent.postMessage({jsonrpc:'2.0',id:m.id,result:{}},'*');return;}
    if(m.method==='ui/notifications/tool-input'||m.method==='ui/notifications/partial-tool-input')this.onInput?.(m.params);
    if(m.method==='ui/notifications/tool-result')this.onResult?.(m.params);
    if(m.method==='ui/notifications/host-context-changed')this.context(m.params);
  }
  context(ctx){
    this.hostContext={...this.hostContext,...ctx};
    const root=document.documentElement,layout=surfaceLayout(this.hostContext);root.dataset.surfaceLayout=layout.kind;root.dataset.displayMode=layout.displayMode;root.dataset.heightBounded=String(layout.maxHeight!=null);
    for(const [key,value]of [['--codexm-host-height',layout.height],['--codexm-host-max-height',layout.maxHeight]]){if(value!=null)root.style.setProperty(key,`${value}px`);else root.style.removeProperty?.(key);}
    if(ctx?.theme)root.dataset.theme=ctx.theme;for(const [key,value] of Object.entries(ctx?.styles?.variables||{})){if(key.startsWith('--')&&typeof value==='string')root.style.setProperty(key,value);}
    this.onContext?.(ctx);this.queueSize();
  }
  queueSize(){
    if(this.sizeFrame!=null||typeof document.body?.getBoundingClientRect!=='function')return;
    const schedule=typeof window.requestAnimationFrame==='function'?window.requestAnimationFrame.bind(window):fn=>setTimeout(fn,0);
    this.sizeFrame=schedule(()=>{this.sizeFrame=null;if(document.hidden)return;const size=panelSize(document.body.getBoundingClientRect(),document.documentElement.clientWidth);if(!size)return;const key=`${size.width}:${size.height}`;if(this.lastSize===key)return;this.lastSize=key;window.parent.postMessage({jsonrpc:'2.0',method:'ui/notifications/size-changed',params:size},'*');});
  }
  request(method,params,timeout=120000){return new Promise((resolve,reject)=>{const id=++this.id;const timer=setTimeout(()=>{this.pending.delete(id);reject(new Error('연결 응답을 확인하지 못했습니다. 작업 상태를 확인한 후 다시 시도하세요.'));},timeout);this.pending.set(id,{resolve,reject,timer});window.parent.postMessage({jsonrpc:'2.0',id,method,params},'*');});}
  async initialize(){
    try{const r=await this.request('ui/initialize',{appInfo:{name:'codexm',version:'0.6.1'},appCapabilities:{availableDisplayModes:['inline','fullscreen']},protocolVersion:'2026-01-26'},8000);this.context(r.hostContext);window.parent.postMessage({jsonrpc:'2.0',method:'ui/notifications/initialized'},'*');}
    catch(e){if(typeof window.openai?.callTool!=='function')throw e;this.compat=true;this.context({displayMode:window.openai.displayMode||'inline'});}
    this.resize=new ResizeObserver(()=>this.queueSize());this.resize.observe(document.body);window.addEventListener('resize',()=>this.queueSize());this.queueSize();
    void this.reportReady();
    document.addEventListener('visibilitychange',()=>{if(!document.hidden){void this.reportReady();this.queueSize();}});
  }
  async reportReady(){try{const args={ui_build:UI_BUILD,bridge:this.compat?'legacy':'mcp-app',visible:!document.hidden};const r=this.compat?await window.openai.callTool('codexm_ui_ready',args):await this.request('tools/call',{name:'codexm_ui_ready',arguments:args},2000);return r?.isError!==true;}catch{return false;}}
  async call(name,args={}){const r=this.compat?await window.openai.callTool(name,args):await this.request('tools/call',{name,arguments:args});if(r?.isError)throw new Error(r.content?.find(x=>x.type==='text')?.text||'관리 요청 실패');return r?._meta?.codexm_data ?? r?.structuredContent?.codexm_data ?? r?.codexm_data ?? r;}
}

export function startUI(options={}){
  const $=id=>document.getElementById(id), bridge=options.bridge||new Bridge();
  const state={snapshot:null,tab:'tasks',selected:'',details:null,detailKey:'',drafts:new Map(),history:new Map(),historyCursor:new Map(),stars:[],busy:false,polling:false,detailSequence:0,detailInFlight:new Map(),olderTurns:[],olderCursor:undefined,logFilters:{scope:'',account:'',period:'0'},conversationSignature:'',controlsSignature:'',logs:null,settings:null,listLimit:300,listFilter:'',focusComposer:false,accountsView:null,accountsSignature:'',offlineSignature:'',initialViewApplied:false,login:null,loginView:null,loginTimer:null,loginSequence:0,loginInFlight:false,loopTimer:null,disposed:false};
  const el=(tag,text,cls)=>{const n=document.createElement(tag);if(text!=null)n.textContent=String(text);if(cls)n.className=cls;return n;};
  const button=(text,fn,cls)=>{const b=el('button',text,cls);b.type='button';b.addEventListener('click',fn);return b;};
  const option=(select,value,text)=>{const n=el('option',text);n.value=value;select.append(n);};
  const field=(form,text,input)=>{const l=el('label',null,'field');l.append(el('span',text),input);form.append(l);return input;};
  const input=(value='',type='text')=>{const i=el('input');i.type=type;i.value=value;return i;};
  const select=(values,current)=>{const s=el('select');for(const v of values)option(s,typeof v==='string'?v:v[0],typeof v==='string'?v:v[1]);s.value=current??'';return s;};
  const message=(text,error=false)=>{$('notice').textContent=text;$('notice').className=error?'error':'';};
  const entries=()=>state.snapshot?.catalog?.sessions||[];
  const selected=()=>entries().find(e=>entryKey(e)===state.selected);
  const job=()=>state.snapshot?.status?.jobs?.find(j=>j.id===selected()?.job_id);
  const canManage=()=>state.snapshot?.manager_api>=1;
  const uid=()=>globalThis.crypto?.randomUUID?.()||`ui-${Date.now()}-${Math.random().toString(16).slice(2)}`;
  async function action(fn){if(state.busy)return;state.busy=true;try{await fn();}catch(e){message(e.message,true);}finally{state.busy=false;await refresh();}}
  const read=(command,args={})=>bridge.call('codexm_read',{command,...args});
  async function control(command,args){if(!canManage())throw new Error('관리 서비스 업데이트가 필요합니다. 기존 작업을 보존하는 서비스 인계를 먼저 완료하세요.');return bridge.call('codexm_control',{command,args});}
  function savedUI(){try{const v=window.openai?.widgetState;if(v){state.stars=Array.isArray(v.stars)?v.stars:[];state.selected=v.selected||'';for(const [k,vv]of Object.entries(v.drafts||{}))if(typeof vv==='string')state.drafts.set(k,vv);}}catch{}}
  function persist(){if(typeof window.openai?.setWidgetState==='function')window.openai.setWidgetState({selected:state.selected,stars:state.stars,drafts:Object.fromEntries(state.drafts)});}
  function closeDialog(){state.loginSequence++;clearTimeout(state.loginTimer);if(loginPending(state.login))void cancelLogin(true);state.loginView=null;$('dialog').close();if(!$('message').disabled)$('message').focus();}
  function dialog(title){const d=$('dialog-body');d.replaceChildren(el('h2',title));$('dialog').showModal();return d;}
  function dialogEnd(d,submit){const bar=el('div',null,'toolbar');bar.append(button('취소',closeDialog));if(submit)bar.append(submit);d.append(bar);}
  function setTab(tab){if(state.snapshot?.connected===false&&tab!=='accounts'){message('관리 서비스를 시작하면 작업·사용 기록·설정을 사용할 수 있습니다.');return;}state.tab=tab;for(const t of ['tasks','accounts','logs','settings'])$(t).hidden=t!==tab;for(const b of document.querySelectorAll('[data-tab]'))b.classList.toggle('active',b.dataset.tab===tab);if(tab==='accounts')renderAccounts();if(tab==='logs')void loadLogs();if(tab==='settings')void loadSettings();}
  function initialView(data){const view=data?.initial_view;if(!state.initialViewApplied&&['tasks','accounts','logs','settings'].includes(view)){state.initialViewApplied=true;setTab(view);}}
  let lastDeepLink;
  function applyDeepLink(context){const view=deepLinkView(context),path=context?.['openai/deepLink']?.url;if(!view||path===lastDeepLink)return;lastDeepLink=path;state.initialViewApplied=true;setTab(view);}
  async function refresh(){
    if(state.disposed||state.polling)return;state.polling=true;
    try{
      const next=await bridge.call('codexm_snapshot');if(state.disposed)return;state.snapshot=next;
      const snap=state.snapshot;
      initialView(snap);$('app').hidden=false;$('offline').hidden=!!snap.connected;$('accounts-strip').hidden=!snap.connected;
      for(const b of document.querySelectorAll('[data-tab]'))b.disabled=!snap.connected&&b.dataset.tab!=='accounts';
      $('connection').textContent=snap.connected?'● 관리 서비스 연결됨':'○ 관리 서비스 중지';
      $('new-button').disabled=!snap.connected||!canManage();
      if(!snap.connected){if(state.tab!=='accounts')setTab('accounts');renderOffline();renderAccounts();return;}
      $('observed').textContent=`마지막 관측 ${date(snap.at)}`;
      $('new-button').disabled=!canManage();
      if(!canManage())message('연결된 관리 서비스가 이전 버전입니다. 업데이트/인계 후 세션 변경과 설정 편집이 활성화됩니다.');
      renderStrip();renderProjects();renderList();renderTask();if(state.tab==='accounts')renderAccounts();
      if(state.selected && state.tab==='tasks' && !state.busy)await loadDetails();
    }catch(e){$('connection').textContent='○ 연결 확인 필요';message(e.message,true);}
    finally{state.polling=false;}
  }
  function renderOffline(){
    const d=$('offline'),snap=state.snapshot||{},names=snap.account_names||[],signature=JSON.stringify([names,snap.manager_account,snap.setup_required,snap.message]);if(signature===state.offlineSignature)return;state.offlineSignature=signature;
    const previous=d.querySelector('select')?.value;d.replaceChildren(el('h2','로컬 관리자 연결'),el('p',snap.message||'계정을 준비한 뒤 관리 서비스를 시작하세요.'));
    if(!names.length){d.append(el('p','아래 계정 화면에서 계정을 추가하고 공식 로그인 페이지에서 인증하세요.','hint'));return;}
    const s=field(d,snap.setup_required?'처음 사용할 관리자 계정':'관리자 계정',select(names,names.includes(previous)?previous:snap.manager_account||names[0]));
    d.append(button('관리 서비스 시작',()=>action(async()=>{await bridge.call('codexm_start',{manager_account:s.value});message('관리 서비스에 연결했습니다.');}),'primary'));
  }
  function accountCard(name){
    const snap=state.snapshot?.status?.accounts?.find(x=>x.account===name),q=quota(snap),card=el('div',null,'account-card');card.dataset.fallback=String(name===state.snapshot?.fallback_account);
    const head=el('div');head.append(el('strong',name));if(name===state.snapshot?.fallback_account)head.append(el('span',' 예비 계정','badge'));card.append(head);
    const usable=snap?.authenticated&&!snap?.error,primary=usable?q.primary:null,secondary=usable?q.secondary:null;
    card.append(el('div',primary==null?'미확인':`${primary}%`, 'amount'),el('div',`단기 남음 · 장기 ${secondary==null?'미확인':`${secondary}% 남음`}`,'small'));
    const bar=el('div',null,'meter'),fill=el('i');fill.style.width=`${primary??0}%`;bar.append(fill);card.append(bar);
    const reset=q.bucket.primary?.resetsAt;card.append(el('div',`관측 ${date(snap?.checked_at)}`,'small'));if(reset)card.append(el('div',`재설정 ${date(reset*1000)}`,'small'));if(snap?.error)card.append(el('div','조회 실패 · 값 미확인','small bad'));return card;
  }
  function renderStrip(){$('accounts-strip').replaceChildren(...(state.snapshot.priority||[]).map(accountCard));}
  function renderProjects(){const s=$('project-filter'),values=[...new Set(entries().map(e=>e.cwd).filter(Boolean))].sort(),signature=JSON.stringify(values);if(s.dataset.signature===signature)return;const old=s.value;s.replaceChildren();option(s,'','모든 프로젝트');for(const p of values)option(s,p,p.split('/').filter(Boolean).pop()||p);s.value=old;s.dataset.signature=signature;}
  function renderList(){
    const filter=JSON.stringify([$('search').value,$('state-filter').value,$('project-filter').value,state.stars]);if(filter!==state.listFilter){state.listLimit=300;state.listFilter=filter;}
    const list=filterSessions(entries(),$('search').value,$('state-filter').value,$('project-filter').value,state.stars);$('session-count').textContent=`${list.length}개 대화 · 관리 중 ${entries().filter(e=>e.managed).length}개`;
    const focus=document.activeElement?.dataset?.sessionKey,scroll=$('sessions').scrollTop;const nodes=list.slice(0,state.listLimit).map(e=>{const key=entryKey(e),b=button('',()=>choose(e));b.className='session';b.dataset.sessionKey=key;b.role='option';b.setAttribute('aria-selected',String(key===state.selected));b.append(el('b',`${state.stars.includes(key)?'★ ':''}${e.title}`),el('small',`${label(e.state)} · ${e.account||'계정 미배정'} · ${e.cwd?.split('/').pop()||''}`));b.addEventListener('keydown',ev=>{if(ev.key==='ArrowDown'||ev.key==='ArrowUp'){ev.preventDefault();const adjacent=ev.key==='ArrowDown'?b.nextElementSibling:b.previousElementSibling;adjacent?.focus();}if(ev.key==='Enter'){ev.preventDefault();choose(e);}});return b;});
    $('sessions').replaceChildren(...nodes);if(list.length>state.listLimit)$('sessions').append(button(`더 보기 (${list.length-state.listLimit}개)`,()=>{state.listLimit+=300;renderList();}));if(focus)nodes.find(n=>n.dataset.sessionKey===focus)?.focus();$('sessions').scrollTop=scroll;
  }
  function choose(e){state.drafts.set(state.selected,$('message').value);state.selected=entryKey(e);state.focusComposer=true;state.details=null;state.detailKey='';state.conversationSignature='';state.controlsSignature='';state.detailSequence++;state.olderTurns=[];state.olderCursor=undefined;$('requests').replaceChildren();$('message').value=state.drafts.get(state.selected)||'';persist();renderList();renderTask();void loadDetails();}
  function renderTask(){
    const e=selected(),j=job();
    if(!e){$('task-header').replaceChildren(el('h2','대화를 선택하세요'));$('task-controls').replaceChildren();$('conversation').replaceChildren(el('p','기존 Codex 대화를 선택하거나 새 작업을 만드세요.','empty'));$('message').disabled=true;$('send-button').disabled=true;return;}
    const signature=JSON.stringify([state.selected,e.title,e.state,e.account,j?.target_account,j?.paused,e.managed,state.stars]);
    if(signature!==state.controlsSignature){
      state.controlsSignature=signature;$('task-header').replaceChildren(el('h2',e.title),el('div',e.cwd||'','path'));
      const controls=$('task-controls');controls.replaceChildren(el('span',label(e.state),'badge'));
      controls.append(button(`${e.account||'계정 선택'}${j?.target_account?` → ${j.target_account}`:''}`,()=>accountDialog(e,j)));
      if(e.managed){for(const [cmd,text]of [['pause','일시중지'],['resume-job','재개'],['stop','중지']])controls.append(button(text,()=>action(async()=>{await control(cmd,{id:e.job_id});message(`${e.title}: ${text} 요청됨`);})));if(state.snapshot.manager_api>=2)controls.append(button('관리 해제',()=>action(async()=>{await control('unregister',{id:e.job_id});message('관리 등록을 해제했습니다. 대화는 보존됩니다.');})));}
      else controls.append(button('관리 시작',()=>accountDialog(e,j),'primary'));
      controls.append(button(state.stars.includes(state.selected)?'★':'☆',()=>{state.stars=state.stars.includes(state.selected)?state.stars.filter(k=>k!==state.selected):[...state.stars,state.selected];persist();renderList();renderTask();}));
      if(!canManage())for(const b of controls.querySelectorAll('button'))b.disabled=true;
    }
    $('task-alerts').textContent=[j?.last_error,j?.registration&&j.state==='external_writer'?`관리 등록됨 · 원래 Codex에서 실행 중${j.registration.origin_account?` (${j.registration.origin_account})`:' (현재 계정 미확인)'}. 계정 인계는 원래 실행이 대화를 해제한 뒤 적용됩니다.`:'',j?.registration?.handoff_due&&j.state==='external_writer'?'한도 중단을 감지했습니다. 원래 대화의 쓰기 권한 해제를 기다립니다.':'',j?.target_account?`계정 전환 대기: ${j.account} → ${j.target_account}`:'',j?.goal?`목표 ${label(j.goal.status)} · 사용 ${Number(j.goal.tokensUsed||0).toLocaleString()} 토큰${j.goal.tokenBudget!=null?` / 예산 ${Number(j.goal.tokenBudget).toLocaleString()}`:''}`:''].filter(Boolean).join('\n');
    const editable=e.managed&&canManage()&&!j?.paused&&!j?.target_account&&j?.state!=='external_writer';$('message').disabled=!editable;$('send-button').disabled=!editable;$('input-target').textContent=`추가 지시 대상: ${e.title} · ${e.account||'계정 미배정'}`;
    if(editable&&state.focusComposer&&state.tab==='tasks'&&!$('dialog').open){$('message').focus();state.focusComposer=false;}
  }
  function accountDialog(e,j){
    const target={...e},current=j?.account||e.account||'',d=dialog(e.managed?'세션 계정 변경':'기존 대화 관리');d.append(el('p',e.title),el('p',e.managed?'이 대화의 계정만 바뀝니다. 진행 중인 작업은 중단 확인 후 이어가며, 일시중지 상태는 유지됩니다.':'기존 대화와 목표를 그대로 관리 대상으로 등록합니다. 선택한 계정은 codexm이 실행을 인계할 때 사용합니다. 원래 Codex에서 사용 중이면 대화가 해제될 때까지 기다립니다.','hint'));
    const s=field(d,'사용할 계정',select(e.managed?state.snapshot.priority:[['','자동 (우선순위)'],...state.snapshot.priority],current));d.append(el('p',`${state.snapshot.fallback_account}: 상위 계정이 모두 소진된 경우에 사용하며 ${state.snapshot.preferred_recheck_seconds/60}분마다 복귀를 확인합니다.`,'hint'));
    const operation=uid();dialogEnd(d,button(e.managed?'계정 변경':'관리 시작',()=>{const account=s.value;closeDialog();void action(async()=>{if(target.managed)await control('switch-account',{id:target.job_id,account,expected_account:current,operation_id:operation});else{const registered=await control(state.snapshot.manager_api>=2?'register':'resume',{thread_id:target.thread_id||target.id,account});if(state.snapshot.manager_api<2)await control('manage',{id:registered.id});if(account&&registered.account&&registered.account!==account)await control('switch-account',{id:registered.id,account,expected_account:registered.account,operation_id:operation});choose({...registered,job_id:registered.id});}message('계정 관리 요청을 접수했습니다. 상태와 사용 기록에서 결과를 확인할 수 있습니다.');});},'primary'));
  }
  async function loadDetails(cursor=''){
    const e=selected();if(!e)return;const key=state.selected,sequence=state.detailSequence;
    if(state.detailInFlight.has(key))return state.detailInFlight.get(key);
    const promise=(async()=>{try{const d=await read('details',e.job_id?{id:e.job_id,cursor}:{thread_id:e.thread_id||e.id,cursor});if(sequence!==state.detailSequence||key!==state.selected)return;
      if(cursor){state.olderTurns=mergeTurns(d.thread?.turns||[],state.details?.thread?.turns||[]);state.olderCursor=d.cursor??null;}else if(state.olderCursor!==undefined){state.olderTurns=state.details?.thread?.turns||state.olderTurns;}
      const latest=cursor?state.details:d;
      if(latest?.thread){latest.thread={...latest.thread,turns:mergeTurns(state.olderTurns,latest.thread.turns||[])};if(state.olderCursor!==undefined)latest.cursor=state.olderCursor;}
      state.details=latest||d;state.detailKey=key;renderConversation(state.details,!!cursor);renderRequests(state.details.job);
    }catch(error){if(key===state.selected)message(error.message,true);}finally{state.detailInFlight.delete(key);}})();
    state.detailInFlight.set(key,promise);return promise;
  }
  function renderConversation(d,older){
    const turns=d.thread?.turns||[],signature=JSON.stringify([turns,d.job?.last_output,d.cursor]);if(signature===state.conversationSignature&&!older)return;state.conversationSignature=signature;
    const viewport=$('conversation'),atBottom=viewport.scrollHeight-viewport.scrollTop-viewport.clientHeight<70,oldHeight=viewport.scrollHeight,nodes=[];
    if(d.cursor)nodes.push(button('이전 대화 더 보기',()=>loadDetails(d.cursor)));
    for(const turn of turns){for(const item of turn.items||[]){
      if(item.type==='userMessage'||item.type==='agentMessage'){const text=item.type==='userMessage'?(item.content||[]).filter(c=>c.type==='text').map(c=>c.text).join('\n'):item.text;const m=el('article',null,`message ${item.type==='userMessage'?'user':'assistant'}`);m.append(el('div',item.type==='userMessage'?'사용자':'Codex','role'),el('p',text||''));nodes.push(m);}
      else{const details=el('details');details.append(el('summary',`${item.type} ${item.status||''}`),el('pre',[item.command,item.aggregatedOutput,...(item.changes||[]).map(c=>`${c.path}\n${c.diff}`)].filter(Boolean).join('\n')||'실행 항목'));nodes.push(details);}
    }if(turn.error)nodes.push(el('p',turn.error.message,'bad'));}
    if(d.job?.state==='running'&&d.job.last_output&&!turns.some(t=>(t.items||[]).some(i=>i.text===d.job.last_output)))nodes.push(el('article',d.job.last_output,'message assistant'));
    if(!nodes.length)nodes.push(el('p','이 대화에 아직 메시지가 없습니다.','empty'));
    const oldTop=viewport.scrollTop;viewport.replaceChildren(...nodes);if(older)viewport.scrollTop=oldTop+viewport.scrollHeight-oldHeight;else if(atBottom)viewport.scrollTop=viewport.scrollHeight;else viewport.scrollTop=oldTop;
  }
  function renderRequests(j){
    const root=$('requests'),signature=JSON.stringify([j?.run_id,j?.pending_requests,j?.target_account]);if(root.dataset.signature===signature)return;root.dataset.signature=signature;root.replaceChildren();
    for(const request of j?.pending_requests||[]){const card=el('div',null,'request');card.append(el('strong','작업에서 응답을 기다리고 있습니다.'));const p=request.params||{},reply=result=>action(async()=>{await control('reply',{id:j.id,run_id:j.run_id,request_id:request.id,result});message('응답을 전달했습니다.');await loadDetails();});
      if(request.method==='item/tool/requestUserInput'){const inputs=[];for(const q of p.questions||[]){card.append(el('p',q.question||q.header));let answer;if(q.options?.length){answer=select([['','선택하세요'],...q.options.map(o=>[o.label,`${o.label}${o.description?' · '+o.description:''}`]),['__custom','직접 입력']], '');field(card,q.header||'답변',answer);}else answer=input();const free=input();field(card,'직접 입력',free);inputs.push({q,answer,free});}card.append(button('답변 보내기',()=>{const answers={};for(const x of inputs){const value=x.free.value.trim()||x.answer.value;if(!value||value==='__custom'){message('각 질문에 답변을 입력하세요.',true);return;}answers[x.q.id]={answers:[value]};}void reply({answers});},'primary'));}
      else if(['item/commandExecution/requestApproval','item/fileChange/requestApproval'].includes(request.method)){card.append(el('pre',p.command||p.reason||'변경 승인이 필요합니다.'));const bar=el('div',null,'toolbar');for(const [decision,text]of [['accept','이번 실행 허용'],['decline','거절'],['cancel','취소']])bar.append(button(text,()=>reply({decision})));card.append(bar);}
      else if(request.method==='item/permissions/requestApproval'){card.append(el('p',p.reason||'추가 권한 요청'),el('pre',JSON.stringify(p.permissions||{},null,2)));const bar=el('div',null,'toolbar');bar.append(button('요청한 권한을 이번 턴에 허용',()=>reply({permissions:p.permissions||{},scope:'turn'})),button('거절',()=>reply({permissions:{},scope:'turn'})));card.append(bar);}
      else if(request.method==='mcpServer/elicitation/request'){card.append(el('p',p.message||'추가 정보 요청'));const form=el('form'),fields=[],schema=p.requestedSchema||{},supported=p.mode==='form'&&Object.values(schema.properties||{}).every(v=>['string','number','integer','boolean'].includes(v.type));if(supported){for(const [key,def]of Object.entries(schema.properties||{})){const n=def.enum?select([['','선택하세요'],...def.enum.map(v=>[String(v),String(v)])],def.default??''):input(def.default??'',def.type==='boolean'?'checkbox':['number','integer'].includes(def.type)?'number':'text');if(def.type==='boolean')n.checked=!!def.default;if(def.type==='integer')n.step='1';if(def.minimum!=null)n.min=def.minimum;if(def.maximum!=null)n.max=def.maximum;if(def.minLength!=null)n.minLength=def.minLength;if(def.maxLength!=null)n.maxLength=def.maxLength;if(schema.required?.includes(key))n.required=true;field(form,def.title||key,n);fields.push({key,def,n});}const accept=el('button','입력 전달','primary');accept.type='submit';form.append(accept);form.onsubmit=e=>{e.preventDefault();if(!form.reportValidity())return;const content={};for(const {key,def,n}of fields){if(!n.required&&n.value==='')continue;content[key]=def.type==='boolean'?n.checked:['number','integer'].includes(def.type)?Number(n.value):n.value;}void reply({action:'accept',content});};card.append(form);}else{card.append(el('p','이 요청 형식은 공식 Codex 터미널에서 완료할 수 있습니다.','hint'));if(p.mode==='url')card.append(el('pre',p.url));}const bar=el('div',null,'toolbar');bar.append(button('거절',()=>reply({action:'decline'})),button('취소',()=>reply({action:'cancel'})));card.append(bar);}
      else{card.append(el('p','이 종류의 요청은 공식 Codex 터미널에서 확인하세요.','hint'));}
      if(j.target_account)for(const b of card.querySelectorAll('button'))b.disabled=true;root.append(card);
    }
  }
  function renderAccounts(){
    const d=$('accounts'),snap=state.snapshot||{},names=accountNames(snap),available=snap.login_available===true,signature=JSON.stringify([names,available,!!snap.connected,!!snap.account_removal_available,canManage()]);
    if(signature!==state.accountsSignature||!state.accountsView){
      state.accountsSignature=signature;d.replaceChildren(el('h2','계정 관리'));
      const summary=el('p',null,'hint'),bar=el('div',null,'toolbar'),add=button('계정 추가',()=>loginDialog(),'primary'),reload=button('모든 계정 한도 새로 조회',()=>action(async()=>{await read('accounts');message('계정 한도를 다시 관측했습니다.');})),settings=button('우선순위·관리 설정',()=>setTab('settings'));
      add.disabled=!available;reload.disabled=!snap.connected;settings.disabled=!snap.connected||!canManage();bar.append(add,reload,settings);d.append(summary,bar);
      if(!available)d.append(el('p','이 연결에서는 GUI 로그인을 사용할 수 없습니다. codexm 플러그인을 갱신한 뒤 다시 열어주세요.','hint'));
      if(!snap.connected)d.append(el('p','계정 추가와 재로그인은 관리 서비스가 중지된 상태에서도 가능합니다. 관리자·예비 계정과 순서는 관리 서비스를 시작한 뒤 설정에서 저장하세요.','hint'));
      const grid=el('div',null,'grid'),cards=new Map();
      for(const name of names){
        const card=el('div',null,'account-card'),metrics=el('div'),roles=el('p',null,'hint'),sessions=el('p',null,'hint'),actions=el('div',null,'toolbar'),login=button('재로그인',()=>loginDialog(name));login.setAttribute('aria-label',`${name} 재로그인`);login.disabled=!available;const remove=button('등록 삭제',()=>{const revision=state.snapshot.config_revision,d=dialog('계정 등록 삭제');d.append(el('p',`${name} 계정을 목록과 작업 우선순위에서 삭제합니다. 로그인 파일·공용 기본 로그인·대화 기록은 보존합니다.`));dialogEnd(d,button('등록 삭제',()=>{closeDialog();void action(async()=>{await bridge.call('codexm_account_remove',{account:name,revision,operation_id:uid()});message('계정 등록을 삭제했습니다. 로그인 파일과 대화는 보존했습니다.');});}));});remove.setAttribute('aria-label',`${name} 등록 삭제`);remove.disabled=!snap.account_removal_available;actions.append(login,remove);card.append(metrics,roles,sessions,actions);grid.append(card);cards.set(name,{card,metrics,roles,sessions});
      }
      if(!names.length)grid.append(el('p','저장된 계정이 없습니다. 계정 추가를 눌러 별칭과 로그인 방식을 선택하세요.','hint'));
      d.append(grid,el('p','비율은 계정 전체 한도입니다. 여러 세션의 사용량이 합쳐져 있으며 한 세션의 정확한 소비 비율을 뜻하지 않습니다.','hint'));
      state.accountsView={summary,cards};
    }
    const view=state.accountsView,status=snap.status||{},manager=status.manager_account||snap.manager_account,native=snap.native_default_account||status.native_default_account;
    view.summary.textContent=`우선순위: ${(snap.priority||[]).join(' → ')||'미설정'} · 관리자: ${manager||'미설정'} · GUI 기본 로그인: ${native||'미확인'}`;
    for(const [name,refs]of view.cards){
      const fresh=accountCard(name);refs.metrics.replaceChildren(...fresh.children);refs.card.dataset.fallback=String(name===snap.fallback_account);
      const index=(snap.priority||[]).indexOf(name),roles=[index>=0?`우선순위 ${index+1}`:'자동 선택에서 제외',name===manager?'관리자':'',name===native?'GUI 기본 로그인':'',name===snap.fallback_account?'예비 계정':''];refs.roles.textContent=roles.filter(Boolean).join(' · ');
      const jobs=(status.jobs||[]).filter(j=>j.account===name);refs.sessions.textContent=`관리 세션 ${jobs.length}개 · 실행 중 ${jobs.filter(j=>j.state==='running').length}개`;
    }
  }
  async function openLoginPage(){
    const url=safeLoginURL(state.login?.auth_url||state.login?.verification_url);if(!url){message('공식 로그인 페이지 주소를 확인하지 못했습니다. 요청을 취소하고 다시 시작하세요.',true);return;}
    try{const result=bridge.compat&&typeof window.openai?.openExternal==='function'?await window.openai.openExternal({href:url,redirectUrl:false}):await bridge.request('ui/open-link',{url},10000);if(result?.isError)throw new Error('link denied');}catch{message('공식 로그인 페이지를 열지 못했습니다. 잠시 후 다시 눌러주세요.',true);}
  }
  function renderLogin(){
    const view=state.loginView,login=state.login;if(!view||!login||!$('dialog').open)return;
    if(!loginPending(login)){delete login.auth_url;delete login.verification_url;delete login.user_code;}
    view.form.hidden=true;view.warning.textContent='';view.status.replaceChildren(el('strong',({pending:'공식 페이지에서 로그인 대기',verifying:'인증 확인 중',succeeded:'로그인 완료',failed:'로그인 실패',canceled:'로그인 취소됨',expired:'로그인 요청 종료'})[login.status]||'로그인 상태 확인 중'));
    view.actions.replaceChildren();
    if(loginPending(login)){
      view.status.append(el('p',`${login.account} 계정의 공식 Codex 인증을 완료하세요. 이 화면이 자동으로 확인합니다.`,'hint'));
      if(login.user_code)view.status.append(el('p','공식 로그인 페이지에 입력할 인증 코드'),el('code',login.user_code));
      const link=button('공식 로그인 페이지 열기',openLoginPage,'primary');link.disabled=!safeLoginURL(login.auth_url||login.verification_url);view.actions.append(link,button('로그인 취소',()=>void cancelLogin()));
      const deadline=login.deadline?date(login.deadline):'시작 후 최대 15분';view.status.append(el('p',`이 창의 로그인 요청 종료: ${deadline}. codexm의 관리 제한이며 공식 인증 코드의 만료 시각과는 다를 수 있습니다.`,'hint'));
      if(link.disabled)view.status.append(el('p','공식 로그인 페이지 주소를 확인하지 못했습니다. 로그인 요청을 취소하고 다시 시작하세요.','bad'));
    }else{
      if(login.status==='succeeded')view.status.append(el('p',`${login.account} 계정을 저장했습니다. 계정 순서와 관리자·기본 계정은 설정에서 별도로 선택하세요.`,'hint'));
      else if(login.error)view.status.append(el('p',login.error,'bad'));
      view.actions.append(button('닫기',closeDialog));if(['failed','expired','canceled'].includes(login.status))view.actions.append(button('다시 시도',()=>loginDialog(login.account)));
    }
  }
  function scheduleLoginPoll(sequence){
    clearTimeout(state.loginTimer);if(state.disposed||!loginPending(state.login)||!$('dialog').open)return;
    state.loginTimer=setTimeout(()=>void pollLogin(sequence),2000);
  }
  async function pollLogin(sequence){
    const current=state.login;if(state.disposed||sequence!==state.loginSequence||!loginPending(current)||!$('dialog').open)return;
    if(document.hidden){scheduleLoginPoll(sequence);return;}
    try{
      const update=await bridge.call('codexm_account',{command:'status',login_id:current.login_id});
      if(state.disposed||sequence!==state.loginSequence||state.login?.login_id!==current.login_id)return;
      state.login={...current,...update};if(!loginPending(state.login)){delete state.login.auth_url;delete state.login.verification_url;delete state.login.user_code;}
      renderLogin();if(state.login.status==='succeeded')await refresh();
    }catch{if(sequence===state.loginSequence&&state.loginView)state.loginView.warning.textContent='로그인 상태를 확인하지 못했습니다. 같은 요청의 상태를 다시 확인합니다.';}
    scheduleLoginPoll(sequence);
  }
  async function cancelLogin(silent=false){
    const current=state.login;if(!loginPending(current))return;
    clearTimeout(state.loginTimer);
    try{const update=await bridge.call('codexm_account',{command:'cancel',login_id:current.login_id});if(state.login?.login_id===current.login_id){state.login={...current,...update};delete state.login.auth_url;delete state.login.verification_url;delete state.login.user_code;if(!silent)renderLogin();}}
    catch{if(!silent){message('로그인 취소 응답을 확인하지 못했습니다. 같은 요청의 상태를 다시 확인합니다.',true);scheduleLoginPoll(state.loginSequence);}}
  }
  function loginDialog(account=''){
    if(loginPending(state.login)&&$('dialog').open){$('dialog').focus();return;}
    clearTimeout(state.loginTimer);state.login=null;const sequence=++state.loginSequence,d=dialog(account?'계정 재로그인':'계정 추가'),form=el('form'),alias=input(account);alias.pattern='[A-Za-z0-9][A-Za-z0-9_-]{0,63}';alias.maxLength=64;alias.required=true;alias.readOnly=!!account;alias.autocomplete='off';field(form,'계정 별칭',alias);
    const mode=field(form,'로그인 방식',select([['browser','브라우저 로그인'],['device','인증 코드로 로그인']],'browser'));form.append(el('p','별칭은 영문·숫자로 시작하고 영문·숫자·밑줄·하이픈을 사용할 수 있습니다. 인증은 공식 Codex 페이지에서 직접 진행합니다.','hint'));
    const submit=el('button','로그인 시작','primary');submit.type='submit';const formActions=el('div',null,'toolbar');formActions.append(button('닫기',closeDialog),submit);form.append(formActions);
    const status=el('div'),warning=el('p',null,'hint'),actions=el('div',null,'toolbar');status.setAttribute('role','status');status.setAttribute('aria-live','polite');warning.setAttribute('role','status');d.append(form,status,warning,actions);state.loginView={form,status,warning,actions};
    form.onsubmit=async event=>{
      event.preventDefault();if(state.loginInFlight||!form.reportValidity())return;const name=alias.value.trim();if(name==='default'){status.textContent='별칭 default는 사용할 수 없습니다.';alias.focus();return;}if(!/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(name)){status.textContent='계정 별칭을 확인하세요.';alias.focus();return;}
      state.loginInFlight=true;submit.disabled=true;status.textContent='공식 로그인 요청을 준비합니다.';
      try{
        const result=await bridge.call('codexm_account',{command:'start',account:name,mode:mode.value});
        if(state.disposed||sequence!==state.loginSequence||!$('dialog').open||!d.contains(form)){if(loginPending(result))void bridge.call('codexm_account',{command:'cancel',login_id:result.login_id}).catch(()=>{});return;}
        state.login=result;renderLogin();scheduleLoginPoll(sequence);if(result.status==='succeeded')await refresh();
      }catch(error){if(sequence===state.loginSequence&&$('dialog').open)status.textContent=error.message||'로그인을 시작하지 못했습니다.';}
      finally{state.loginInFlight=false;submit.disabled=false;}
    };
    if(!account)alias.focus();
  }
  async function loadLogs(){try{state.logs=await read('logs',{limit:500});if(state.tab==='logs')renderLogs();}catch(e){message(e.message,true);}}
  function renderLogs(){
    const d=$('logs');d.replaceChildren(el('h2','사용 기록'),el('p','계정 전환 시점과 사유, 세션 실행 시간, 한도 관측을 함께 봅니다.','hint'));
    const scope=select([['','전체 세션'],...state.snapshot.status.jobs.map(j=>[j.id,j.title])],state.logFilters.scope),account=select([['','모든 계정'],...state.snapshot.priority.map(n=>[n,n])],state.logFilters.account),period=select([['0','모든 기간'],['1','최근 24시간'],['7','최근 7일']],state.logFilters.period),bar=el('div',null,'toolbar');scope.setAttribute('aria-label','로그 세션');account.setAttribute('aria-label','로그 계정');period.setAttribute('aria-label','로그 기간');bar.append(scope,account,period,button('새로고침',loadLogs));if(selected()?.job_id)bar.append(button('선택한 작업만',()=>{scope.value=selected().job_id;draw();}));d.append(bar);const content=el('div');d.append(content);
    function draw(){state.logFilters={scope:scope.value,account:account.value,period:period.value};const from=period.value==='0'?0:Date.now()-Number(period.value)*86400000;content.replaceChildren();const log=state.logs||{},runs=(log.runs||[]).filter(r=>(!scope.value||r.JobID===scope.value)&&(!account.value||r.Account===account.value)&&(+new Date(r.EndedAt||Date.now())>=from));const wrap=el('div',null,'table-wrap'),table=el('table'),head=el('tr');for(const text of ['세션 / 계정','시작','실행 시간','시작 잔여','최근 잔여'])head.append(el('th',text));table.append(head);for(const r of runs){const row=el('tr'),name=state.snapshot.status.jobs.find(j=>j.id===r.JobID)?.title||r.JobID,percent=u=>u?`${remaining(u.primary)==null?'미관측':remaining(u.primary)+'%'} / ${remaining(u.secondary)==null?'미관측':remaining(u.secondary)+'%'}`:'미관측';for(const text of [`${name} · ${r.Account}`,date(r.StartedAt),duration(r.StartedAt,r.EndedAt||Date.now()),percent(r.UsageStart),percent(r.UsageLatest)])row.append(el('td',text));table.append(row);}wrap.append(table);content.append(wrap,el('p',log.note||'한도는 계정 전체의 관측값이며 세션별 정확한 소비율이 아닙니다.','hint'),el('h3','계정 전환'));const timeline=el('div',null,'timeline');for(const c of(log.changes||[]).filter(c=>(!scope.value||c.job_id===scope.value)&&(!account.value||c.from===account.value||c.to===account.value)&&+new Date(c.at)>=from)){const n=el('div');n.append(el('strong',`${c.from} → ${c.to}`),el('p',c.reason),el('small',`${date(c.at)} · ${state.snapshot.status.jobs.find(j=>j.id===c.job_id)?.title||c.job_id}`));timeline.append(n);}if(!timeline.children.length)timeline.append(el('p','이 조건의 전환 기록이 없습니다.','hint'));content.append(timeline);}
    for(const s of [scope,account,period])s.onchange=draw;draw();
  }
  async function loadSettings(){if(!canManage())return;try{const doc=await read('settings-edit');if(state.tab!=='settings')return;state.settings=doc;renderSettings(doc);}catch(e){message(e.message,true);}}
  function renderSettings(doc){
    const d=$('settings');d.replaceChildren(el('h2','관리 설정'),el('p',doc.application,'hint'));const form=el('form'),values=structuredClone(doc.values),names=doc.accounts||[],manager=field(form,'관리자 계정 (명시적으로 관리자 호출할 때 사용)',select(names,values.manager_account)),fallback=field(form,'예비 계정 (항상 마지막 우선순위)',select([['','사용하지 않음'],...names.map(n=>[n,n])],values.fallback_account));
    form.append(el('p','별도 기본 작업 계정은 없습니다. 자동 작업은 아래 순서에서 사용 가능한 계정을 선택합니다. GUI 기본 로그인은 이 순서와 별개입니다.','hint'),el('h3','작업 계정 우선순위'));const order=el('div');form.append(order);function orderView(){order.replaceChildren();values.priority.forEach((name,index)=>{const row=el('div',null,'priority-row');row.append(el('span',`${index+1}. ${name}`));for(const [delta,text]of [[-1,'위'],[1,'아래']]){const b=button(text,()=>{const next=index+delta;if(next<0||next>=values.priority.length)return;[values.priority[index],values.priority[next]]=[values.priority[next],values.priority[index]];orderView();});b.disabled=index+delta<0||index+delta>=values.priority.length;row.append(b);}row.append(button('제외',()=>{values.priority.splice(index,1);orderView();}));order.append(row);});}orderView();const add=select([['','계정 추가'],...names.map(n=>[n,n])],'');add.onchange=()=>{if(add.value&&!values.priority.includes(add.value)){values.priority.push(add.value);orderView();}add.value='';};form.append(add);
    const grid=el('div',null,'grid'),fields={};for(const [key,text,min,max]of [['preferred_recheck_seconds','예비 계정에서 복귀 확인 (초)',60,86400],['reconcile_interval_seconds','실행 상태 대조 주기 (초)',1,300],['max_parallel_jobs','동시 실행 작업 수',1,32],['usage_sample_seconds','사용량 관측 간격 (초)',10,3600]]){const i=input(values[key],'number');i.min=min;i.max=max;i.required=true;fields[key]=field(grid,text,i);}form.append(grid,el('p','예비 계정은 상위 계정이 모두 소진된 경우에만 사용합니다. 조회 실패를 소진으로 취급하지 않습니다.','hint'));const alerts=input('','checkbox');alerts.checked=!!values.five_hour_discord;field(form,'5시간 한도 소진 시 Discord 알림 (전체 계정과 현재 작업 현황 포함)',alerts);const save=el('button','설정 저장','primary');save.type='submit';const actions=el('div',null,'toolbar');actions.append(save,button('저장된 값 다시 불러오기',loadSettings));form.append(actions);form.onsubmit=ev=>{ev.preventDefault();if(!form.reportValidity())return;void action(async()=>{const next={...values,manager_account:manager.value,fallback_account:fallback.value,five_hour_discord:alerts.checked};for(const [key,i]of Object.entries(fields))next[key]=Number(i.value);const saved=await bridge.call('codexm_settings_save',{revision:doc.revision,values:next});state.settings=saved;renderSettings(saved);message('설정을 저장했습니다. 기존 세션 계정은 유지됩니다.');});};d.append(form);
  }
  async function newDialog(){
    const d=dialog('새 작업'),form=el('form'),cwd=field(form,'작업 폴더 (절대 경로)',input(selected()?.cwd||'')),title=field(form,'제목',input()),account=field(form,'작업 계정',select([['','자동 (우선순위)'],...state.snapshot.priority],'')),kind=field(form,'작업 종류',select([['chat','일반 대화'],['task','완료 조건 없는 작업'],['goal','목표를 이어가는 작업']],'chat')),model=field(form,'모델',select([['','Codex 기본값']],'')),goal=field(form,'목표 (목표 작업인 경우 필수)',input()),prompt=field(form,'첫 지시',el('textarea'));cwd.required=true;const submit=el('button','작업 시작','primary');submit.type='submit';d.append(form);const bar=el('div',null,'toolbar');bar.append(button('취소',closeDialog),submit);form.append(bar);form.onsubmit=ev=>{ev.preventDefault();if(!form.reportValidity())return;if(kind.value==='goal'&&!goal.value.trim()){message('목표를 입력하세요.',true);goal.focus();return;}const args={cwd:cwd.value,title:title.value,account:account.value,kind:kind.value,prompt:prompt.value};if(model.value)args.model=model.value;if(kind.value==='goal')args.goal=goal.value;closeDialog();void action(async()=>{const j=await control('new',args);choose({...j,job_id:j.id});setTab('tasks');message('새 작업을 생성했습니다.');});};
    try{const list=await read('models');if(d.contains(model)&&$('dialog').open){for(const m of list.data||[])option(model,m.model||m.id,m.displayName||m.model||m.id);}}catch{if($('dialog').open)d.append(el('p','모델 목록을 확인하지 못했습니다. Codex 기본값으로 시작할 수 있습니다.','hint'));}
  }
  $('composer').onsubmit=ev=>{ev.preventDefault();const e=selected(),j=job(),text=$('message').value;if(!e?.managed||!text.trim()||state.busy)return;const target=state.selected,steer=$('steer').checked;void action(async()=>{await control('send',{id:e.job_id,text,steer});const history=state.history.get(target)||[];history.push(text);state.history.set(target,history.slice(-100));state.drafts.delete(target);if(state.selected===target)$('message').value='';persist();message('지시를 접수했습니다.');});};
  $('message').addEventListener('input',()=>{state.drafts.set(state.selected,$('message').value);state.historyCursor.delete(state.selected);persist();});
  $('message').addEventListener('keydown',ev=>{if(ev.isComposing||ev.keyCode===229)return;const box=$('message'),history=state.history.get(state.selected)||[];if(!ev.shiftKey&&history.length&&box.selectionStart===box.selectionEnd&&((ev.key==='ArrowUp'&&box.selectionStart===0)||(ev.key==='ArrowDown'&&box.selectionEnd===box.value.length&&state.historyCursor.has(state.selected)))){ev.preventDefault();let index=state.historyCursor.get(state.selected)??history.length;if(!state.historyCursor.has(state.selected))state.drafts.set(state.selected,box.value);index=Math.max(0,Math.min(history.length,index+(ev.key==='ArrowUp'?-1:1)));state.historyCursor.set(state.selected,index);box.value=index===history.length?(state.drafts.get(state.selected)||''):history[index];box.setSelectionRange(ev.key==='ArrowUp'?0:box.value.length,ev.key==='ArrowUp'?0:box.value.length);return;}if(ev.key==='Enter'&&!ev.shiftKey){ev.preventDefault();$('composer').requestSubmit();}});
  $('history-button').onclick=()=>{const target=state.selected,d=dialog('이 세션의 입력 기록');for(const text of [...(state.history.get(target)||[])].reverse()){d.append(button(text.slice(0,120),()=>{if(state.selected===target){$('message').value=text;state.drafts.set(target,text);}closeDialog();}));}if(!d.querySelector('button'))d.append(el('p','이 화면에서 보낸 지시가 아직 없습니다.','hint'));dialogEnd(d);};
  for(const b of document.querySelectorAll('[data-tab]'))b.onclick=()=>setTab(b.dataset.tab);
  $('new-button').onclick=newDialog;for(const id of ['search','state-filter','project-filter'])$(id).addEventListener(id==='search'?'input':'change',renderList);
  bridge.onResult=r=>{const data=r?._meta?.codexm_data||r?.structuredContent?.codexm_data;initialView(data);if(data?.connected!=null&&!state.snapshot){state.snapshot=data;void refresh();}};
  bridge.onInput=params=>initialView(params?.arguments||params?.toolInput||params);
  bridge.onContext=applyDeepLink;
  function dispose(){if(state.disposed)return;state.disposed=true;state.loginSequence++;clearTimeout(state.loopTimer);clearTimeout(state.loginTimer);if(loginPending(state.login))void cancelLogin(true);state.loginView=null;bridge.resize?.disconnect?.();window.removeEventListener?.('pagehide',dispose);}
  $('dialog').addEventListener('cancel',event=>{event.preventDefault();closeDialog();});window.addEventListener('pagehide',dispose);
  savedUI();initialView(window.openai?.toolInput);
  const ready=(async()=>{try{await bridge.initialize();applyDeepLink(bridge.hostContext);if(!state.disposed)await refresh();}catch(e){if(!state.disposed){message('이 호스트의 MCP Apps 연결을 확인할 수 없습니다. '+e.message,true);$('connection').textContent='연결 지원 확인 필요';}}async function loop(){if(state.disposed)return;if(!document.hidden)await refresh();if(!state.disposed)state.loopTimer=setTimeout(loop,options.pollInterval??4000);}if(!state.disposed&&options.pollInterval!==0)state.loopTimer=setTimeout(loop,options.pollInterval??4000);})();
  return {state,refresh,ready,dispose};
}
if(typeof document!=='undefined'&&document.getElementById('app'))startUI();
