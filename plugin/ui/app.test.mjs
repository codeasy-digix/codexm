import test from 'node:test';
import assert from 'node:assert/strict';
import {remaining, quota, filterSessions, mergeTurns, Bridge} from './app.mjs';

test('unknown quota is not displayed as free capacity; named bucket takes precedence',()=>{
  assert.equal(remaining(null),null);
  assert.equal(remaining({usedPercent:null}),null);
  assert.equal(remaining({usedPercent:0}),100);
  assert.equal(remaining({usedPercent:150}),0);
  const q=quota({rateLimits:{primary:{usedPercent:0}},rateLimitsByLimitId:{codex:{primary:{usedPercent:90}}}});
  assert.equal(q.primary,10);assert.equal(q.secondary,null);
});
test('task filters retain native conversations and use stable managed IDs',()=>{
  const entries=[{job_id:'a',thread_id:'thread-a',title:'First',managed:true,state:'running',cwd:'/one',account:'account-secondary'},{thread_id:'b',title:'Native',state:'saved',cwd:'/two'}];
  assert.equal(filterSessions(entries,'','all','').length,2);
  assert.equal(filterSessions(entries,'account-secondary','managed','/one')[0].job_id,'a');
  assert.equal(filterSessions(entries,'','starred','',['b'])[0].thread_id,'b');
});
test('older history survives live refresh and duplicate turns receive the latest state',()=>{
  const first=[{id:'old',items:['old']},{id:'live',items:['partial']}];
  const latest=[{id:'live',items:['done']},{id:'new',items:[]}];
  assert.deepEqual(mergeTurns(first,latest),[first[0],...latest]);
});

function bridgeHost(t,{reportError=false}={}) {
  const keys=['window','document','ResizeObserver'],previous=new Map(keys.map(key=>[key,Object.getOwnPropertyDescriptor(globalThis,key)])),messages=[];
  const listeners=new Map(),parent={postMessage(message){messages.push(message);if(message.id==null)return;queueMicrotask(()=>{
    const result=message.method==='ui/initialize'?{hostContext:{theme:'dark'}}:{isError:reportError};
    listeners.get('message')({source:parent,data:{jsonrpc:'2.0',id:message.id,result}});
  });}};
  globalThis.window={parent,addEventListener(name,fn){listeners.set(name,fn);}};
  globalThis.document={hidden:false,documentElement:{dataset:{},style:{setProperty(){}},scrollHeight:600,clientWidth:500},body:{},addEventListener(name,fn){listeners.set(name,fn);}};
  globalThis.ResizeObserver=class{observe(){}};
  t.after(()=>{for(const key of keys){const old=previous.get(key);if(old)Object.defineProperty(globalThis,key,old);else delete globalThis[key];}});
  return {messages,listeners,parent};
}

test('panel handshake reports the actual bridge without blocking an older host',async t=>{
  const {messages,listeners,parent}=bridgeHost(t,{reportError:true}),bridge=new Bridge();
  await bridge.initialize();
  await new Promise(resolve=>setImmediate(resolve));
  assert.equal(document.documentElement.dataset.theme,'dark');
  const contexts=[];bridge.onContext=context=>contexts.push(context);
  listeners.get('message')({source:parent,data:{jsonrpc:'2.0',method:'ui/notifications/host-context-changed',params:{'openai/deepLink':{url:'/accounts'}}}});
  assert.equal(contexts[0]['openai/deepLink'].url,'/accounts');
  assert.ok(messages.some(m=>m.method==='ui/notifications/initialized'));
  const report=messages.find(m=>m.method==='tools/call'&&m.params.name==='codexm_ui_ready');
  assert.equal(report.params.arguments.bridge,'mcp-app');
  assert.equal(report.params.arguments.visible,true);
  assert.equal(await bridge.reportReady(),false);
  // An unrelated frame cannot inject a host context or resolve pending calls.
  listeners.get('message')({source:{},data:{jsonrpc:'2.0',method:'ui/notifications/host-context-changed',params:{theme:'light'}}});
  assert.equal(document.documentElement.dataset.theme,'dark');
  document.hidden=true;
  listeners.get('visibilitychange')();
  const before=messages.length;
  document.hidden=false;
  listeners.get('visibilitychange')();
  await new Promise(resolve=>setImmediate(resolve));
  assert.ok(messages.length>before);
  assert.equal(parent,window.parent);
});

test('legacy bridge remains usable when optional diagnostic reporting fails',async t=>{
  bridgeHost(t);
  window.openai={callTool:async(name)=>{if(name==='codexm_ui_ready')throw Error('old host has no diagnostic tool');return {_meta:{codexm_data:{connected:true}}};}};
  const bridge=new Bridge();
  bridge.request=async()=>{throw Error('no MCP Apps bridge');};
  await bridge.initialize();
  assert.equal(bridge.compat,true);
  assert.equal(await bridge.reportReady(),false);
  assert.deepEqual(await bridge.call('codexm_snapshot'),{connected:true});
});
