import test from 'node:test';
import assert from 'node:assert/strict';
import {accountNames,loginPending,safeLoginURL,startUI} from './app.mjs';

test('account catalog includes saved accounts outside the automatic order',()=>{
  assert.deepEqual(accountNames({priority:['account-secondary','account-work'],account_names:['extra','account-work'],status:{accounts:[{account:'observed'},{account:'../invalid'}]}}),['account-secondary','account-work','extra','observed']);
  assert.equal(loginPending({status:'verifying'}),true);
  assert.equal(loginPending({status:'succeeded'}),false);
});
test('only an official HTTPS sign-in address can be opened',()=>{
  assert.equal(safeLoginURL('https://auth.openai.com/codex/device'),'https://auth.openai.com/codex/device');
  assert.equal(safeLoginURL('https://auth0.openai.com/authorize'),'https://auth0.openai.com/authorize');
  assert.equal(safeLoginURL('https://chatgpt.com/auth/login'),'https://chatgpt.com/auth/login');
  for(const url of ['javascript:alert(1)','data:text/html,x','http://auth.openai.com/','https://auth.openai.com:1234/','https://auth.openai.com.evil.test/','https://evil.test/','https://user:secret@auth.openai.com/'])assert.equal(safeLoginURL(url),null);
});

// This deliberately small DOM runs the real UI event handlers without a browser,
// dependencies, network, auth files, conversations, or a management service.
class Node {
  constructor(tag,doc){this.tagName=tag.toUpperCase();this.doc=doc;this.children=[];this.parentNode=null;this.listeners=new Map();this.dataset={};this.attributes=new Map();this.style={};this.value='';this.hidden=false;this.disabled=false;this.open=false;this.scrollTop=0;this.className='';this._text='';this.classList={toggle:(value,on)=>{const classes=new Set(this.className.split(' ').filter(Boolean));on?classes.add(value):classes.delete(value);this.className=[...classes].join(' ');}};}
  set textContent(value){this._text=String(value);for(const node of this.children)node.parentNode=null;this.children=[];}
  get textContent(){return this._text+this.children.map(node=>node.textContent).join('');}
  append(...nodes){for(const node of nodes){if(node.parentNode)node.parentNode.children=node.parentNode.children.filter(child=>child!==node);node.parentNode=this;this.children.push(node);}}
  replaceChildren(...nodes){this.textContent='';this.append(...nodes);}
  setAttribute(key,value){this.attributes.set(key,String(value));}
  getAttribute(key){return this.attributes.get(key)||null;}
  addEventListener(key,fn){const handlers=this.listeners.get(key)||[];handlers.push(fn);this.listeners.set(key,handlers);}
  async emit(type,extra={}){if(type==='click'&&this.disabled)return;const event={type,preventDefault(){},...extra};const tasks=(this.listeners.get(type)||[]).map(fn=>fn(event));if(typeof this['on'+type]==='function')tasks.push(this['on'+type](event));await Promise.all(tasks);}
  click(){return this.emit('click');}
  contains(node){return this===node||this.children.some(child=>child.contains(node));}
  querySelectorAll(selector){const matches=node=>selector==='[data-tab]'?!!node.dataset.tab:node.tagName.toLowerCase()===selector;return this.children.flatMap(node=>[...(matches(node)?[node]:[]),...node.querySelectorAll(selector)]);}
  querySelector(selector){return this.querySelectorAll(selector)[0]||null;}
  focus(){if(!this.disabled)this.doc.activeElement=this;}
  showModal(){this.open=true;}
  close(){this.open=false;}
  reportValidity(){return [...this.querySelectorAll('input'),...this.querySelectorAll('select')].every(node=>(!node.required||node.value!=='')&&(!node.pattern||new RegExp('^(?:'+node.pattern+')$').test(node.value)));}
}
const settle=()=>new Promise(resolve=>setImmediate(resolve));
function uiFixture(t,{connected=false,names=[],priority=[],toolInput,hostContext,login}={}){
  const old=new Map(['window','document'].map(key=>[key,Object.getOwnPropertyDescriptor(globalThis,key)])),ids=new Map(),listeners=new Map(),calls=[],requests=[],widgetStates=[];
  const doc={hidden:false,createElement(tag){return new Node(tag,doc);},getElementById(id){return ids.get(id);},querySelectorAll(selector){return doc.body.querySelectorAll(selector);}};
  doc.body=doc.createElement('body');doc.activeElement=doc.body;
  const tags={message:'textarea',composer:'form',dialog:'dialog',search:'input','state-filter':'select','project-filter':'select',steer:'input','new-button':'button','send-button':'button','history-button':'button'};
  for(const id of ['app','notice','offline','connection','observed','accounts-strip','tasks','accounts','logs','settings','new-button','project-filter','state-filter','search','sessions','session-count','task-header','task-controls','task-alerts','conversation','requests','message','send-button','input-target','dialog','dialog-body','composer','steer','history-button']){const node=doc.createElement(tags[id]||'div');node.id=id;ids.set(id,node);doc.body.append(node);}
  ids.get('dialog').append(ids.get('dialog-body'));ids.get('state-filter').value='all';ids.get('message').disabled=true;
  for(const tab of ['tasks','accounts','logs','settings']){const node=doc.createElement('button');node.dataset.tab=tab;doc.body.append(node);}
  globalThis.document=doc;
  globalThis.window={addEventListener(key,fn){const handlers=listeners.get(key)||[];handlers.push(fn);listeners.set(key,handlers);},removeEventListener(key,fn){listeners.set(key,(listeners.get(key)||[]).filter(value=>value!==fn));},openai:{toolInput,setWidgetState(value){widgetStates.push(value);}}};
  const snapshot={connected,manager_api:2,login_available:true,account_names:names.slice(),priority:priority.slice(),manager_account:names[0]||'',setup_required:!names.length,at:new Date().toISOString(),catalog:{sessions:[]},status:{accounts:[],jobs:[],manager_account:names[0]||'',default_worker_account:names[0]||''}};
  let current,instance;
  const bridge={compat:false,hostContext,async initialize(){},async request(method,args){requests.push({method,args});return{};},async call(name,args={}){
    calls.push({name,args});
    if(name==='codexm_snapshot')return structuredClone(snapshot);
    if(name==='codexm_account'){
      if(args.command==='start'){if(login?.start)return login.start(args);current={account:args.account,login_id:'opaque-ui-login',mode:args.mode,status:'pending',deadline:new Date(Date.now()+900000).toISOString(),auth_url:'https://auth.openai.com/oauth/authorize?test=private'};if(args.mode==='device'){delete current.auth_url;current.verification_url='https://auth.openai.com/codex/device';current.user_code='PRIVATE-CODE';}return {...current};}
      if(args.command==='status'){if(login?.status)return login.status(args,current,snapshot);return {...current};}
      if(args.command==='cancel'){current={...current,status:'canceled'};delete current.auth_url;delete current.verification_url;delete current.user_code;return {...current};}
    }
    if(name==='codexm_read'&&args.command==='accounts')return snapshot.status.accounts;
    if(name==='codexm_start'){snapshot.connected=true;snapshot.manager_account=args.manager_account;return{started:true};}
    throw Error('Unexpected fixture call '+name);
  }};
  instance=startUI({bridge,pollInterval:0});
  t.after(()=>{instance.dispose();for(const [key,descriptor]of old){if(descriptor)Object.defineProperty(globalThis,key,descriptor);else delete globalThis[key];}});
  return {instance,ids,doc,bridge,snapshot,calls,requests,widgetStates,async emit(key){for(const fn of listeners.get(key)||[])fn();await settle();}};
}
function button(root,text){const result=root.querySelectorAll('button').find(node=>node.textContent===text);assert.ok(result,'button not found: '+text);return result;}

test('official deep links select the requested tab without overriding later navigation on unrelated context updates',async t=>{
  const f=uiFixture(t,{connected:true,names:['fixture'],toolInput:{initial_view:'tasks'},hostContext:{'openai/deepLink':{url:'/accounts'}}});await f.instance.ready;
  assert.equal(f.instance.state.tab,'accounts');
  const tasks=f.doc.querySelectorAll('[data-tab]').find(node=>node.dataset.tab==='tasks');await tasks.click();
  f.bridge.onContext({theme:'light','openai/deepLink':{url:'/accounts'}});assert.equal(f.instance.state.tab,'tasks');
  f.bridge.onContext({'openai/deepLink':{url:'/'}});assert.equal(f.instance.state.tab,'tasks');
  f.bridge.onContext({'openai/deepLink':{url:'/accounts'}});assert.equal(f.instance.state.tab,'accounts');
  f.bridge.onContext({'openai/deepLink':{url:'https://example.invalid'}});assert.equal(f.instance.state.tab,'accounts');
  assert.deepEqual(f.calls.map(call=>call.name),['codexm_snapshot']);
});

test('task deep links cannot bypass the stopped-service account setup',async t=>{
  const f=uiFixture(t,{hostContext:{'openai/deepLink':{url:'/'}}});await f.instance.ready;
  assert.equal(f.instance.state.tab,'accounts');
  f.bridge.onContext({'openai/deepLink':{url:'/accounts'}});
  f.bridge.onContext({'openai/deepLink':{url:'/'}});assert.equal(f.instance.state.tab,'accounts');
});

test('first account can sign in from the stopped service and only explicit start changes the manager',async t=>{
  t.mock.timers.enable({apis:['setTimeout']});
  const f=uiFixture(t,{login:{status(args,current,snapshot){snapshot.account_names.push(current.account);return {...current,status:'succeeded'};}}});await f.instance.ready;
  assert.equal(f.ids.get('app').hidden,false);assert.equal(f.instance.state.tab,'accounts');assert.ok(f.ids.get('offline').textContent.includes('계정을 추가'));
  assert.deepEqual(f.calls.map(call=>call.name),['codexm_snapshot']);
  await button(f.ids.get('accounts'),'계정 추가').click();const form=f.ids.get('dialog-body').querySelector('form');form.querySelector('input').value='new-alias';form.querySelector('select').value='device';await form.emit('submit');
  assert.deepEqual(f.calls.find(call=>call.name==='codexm_account').args,{command:'start',account:'new-alias',mode:'device'});
  assert.ok(f.ids.get('dialog-body').textContent.includes('PRIVATE-CODE'));await button(f.ids.get('dialog-body'),'공식 로그인 페이지 열기').click();
  assert.deepEqual(f.requests,[{method:'ui/open-link',args:{url:'https://auth.openai.com/codex/device'}}]);
  t.mock.timers.tick(2000);await settle();
  assert.ok(f.ids.get('dialog-body').textContent.includes('로그인 완료'));assert.equal(f.instance.state.login.auth_url,undefined);assert.equal(f.instance.state.login.user_code,undefined);
  assert.equal(f.snapshot.connected,false);assert.equal(f.snapshot.manager_account,'');assert.deepEqual(f.snapshot.priority,[]);assert.equal(f.calls.some(call=>['codexm_start','codexm_control','codexm_settings_save'].includes(call.name)),false);
  assert.equal(f.widgetStates.length,0,'login must not write any authorization data to widget state');
  assert.equal(f.ids.get('offline').querySelector('select').value,'new-alias');await button(f.ids.get('offline'),'관리 서비스 시작').click();await settle();assert.equal(f.calls.filter(call=>call.name==='codexm_start').length,1);
});

test('all saved accounts remain manageable and refresh preserves the login form and focus',async t=>{
  const f=uiFixture(t,{connected:true,names:['account-secondary','extra'],priority:['account-secondary'],toolInput:{initial_view:'accounts'}});await f.instance.ready;
  assert.equal(f.instance.state.tab,'accounts');assert.ok(f.ids.get('accounts').textContent.includes('extra'));assert.ok(f.ids.get('accounts').textContent.includes('자동 선택에서 제외'));
  const relogin=f.ids.get('accounts').querySelectorAll('button').find(node=>node.getAttribute('aria-label')==='extra 재로그인');await relogin.click();const form=f.ids.get('dialog-body').querySelector('form'),alias=form.querySelector('input'),mode=form.querySelector('select');assert.equal(alias.readOnly,true);mode.value='device';mode.focus();
  await f.instance.refresh();assert.equal(f.ids.get('dialog-body').querySelector('form'),form);assert.equal(mode.value,'device');assert.equal(f.doc.activeElement,mode);
  assert.equal(f.ids.get('accounts').querySelectorAll('button').find(node=>node.getAttribute('aria-label')==='extra 재로그인'),relogin);
  const tasks=f.doc.querySelectorAll('[data-tab]').find(node=>node.dataset.tab==='tasks');await tasks.click();f.snapshot.initial_view='accounts';await f.instance.refresh();assert.equal(f.instance.state.tab,'tasks');
  assert.equal(f.calls.some(call=>call.name==='codexm_account'),false);
});

test('poll failures keep one warning and cancel never creates another login',async t=>{
  t.mock.timers.enable({apis:['setTimeout']});const f=uiFixture(t,{login:{status(){throw Error('temporary observation failure');}}});await f.instance.ready;
  await button(f.ids.get('accounts'),'계정 추가').click();const form=f.ids.get('dialog-body').querySelector('form');form.querySelector('input').value='retry';await form.emit('submit');const count=f.ids.get('dialog-body').querySelectorAll('p').length;
  for(let n=0;n<3;n++){t.mock.timers.tick(2000);await settle();assert.equal(f.ids.get('dialog-body').querySelectorAll('p').length,count);}
  assert.equal(f.calls.filter(call=>call.name==='codexm_account'&&call.args.command==='start').length,1);
  await button(f.ids.get('dialog-body'),'로그인 취소').click();await settle();assert.equal(f.instance.state.login.status,'canceled');const statusCalls=f.calls.filter(call=>call.args.command==='status').length;t.mock.timers.tick(10000);await settle();assert.equal(f.calls.filter(call=>call.args.command==='status').length,statusCalls);
});

test('closing the frame cancels pending authentication without touching workers',async t=>{
  const f=uiFixture(t);await f.instance.ready;await button(f.ids.get('accounts'),'계정 추가').click();const form=f.ids.get('dialog-body').querySelector('form');form.querySelector('input').value='closing';await form.emit('submit');await f.emit('pagehide');
  assert.equal(f.instance.state.disposed,true);assert.ok(f.calls.some(call=>call.name==='codexm_account'&&call.args.command==='cancel'));assert.equal(f.calls.some(call=>call.name==='codexm_control'||call.name==='codexm_start'),false);
});

test('a start response that arrives after dialog close is canceled rather than hidden in the background',async t=>{
  let resolveStart;const f=uiFixture(t,{login:{start:()=>new Promise(resolve=>{resolveStart=resolve;})}});await f.instance.ready;await button(f.ids.get('accounts'),'계정 추가').click();const form=f.ids.get('dialog-body').querySelector('form');form.querySelector('input').value='delayed';const submitting=form.emit('submit');await settle();await button(form,'닫기').click();resolveStart({account:'delayed',login_id:'delayed-private-id',mode:'browser',status:'pending',auth_url:'https://auth.openai.com/oauth/authorize'});await submitting;await settle();
  assert.equal(f.ids.get('dialog').open,false);assert.ok(f.calls.some(call=>call.args.command==='cancel'&&call.args.login_id==='delayed-private-id'));assert.equal(f.instance.state.login,null);assert.equal(f.requests.length,0);
});
